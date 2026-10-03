package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Imagens compartilhadas. Uma imagem enviada por POST /imagens é salva em disco e
// recebe um id = SHA-256 do CONTEÚDO (primeiros 128 bits, em hexadecimal): duas
// imagens iguais geram o mesmo id e ocupam um arquivo só. O link de compartilhar
// da Web UI leva esse id em vez da imagem inteira.
//
// Validade: 7 dias a partir do último compartilhamento. Enviar a mesma imagem de
// novo renova o prazo (atualiza a data do arquivo). Uma limpeza roda na subida do
// servidor e a cada 24 h apagando o que passou do prazo; um arquivo vencido
// também deixa de ser servido mesmo antes da limpeza.
//
// Onde: <ELGIN_DATA_DIR>/imagens (padrão ./data/imagens).

const (
	imgTTLPadrao        = 7 * 24 * time.Hour
	imgLimpezaIntervalo = 24 * time.Hour
	imgMaxBytes         = 10 << 20  // por imagem (já decodificada do base64)
	imgMaxTotalPadrao   = 512 << 20 // soma de todas as imagens guardadas
)

var (
	dataDir              = "data" // ELGIN_DATA_DIR
	imgTTL               = imgTTLPadrao
	imgMaxTotal    int64 = imgMaxTotalPadrao // ELGIN_IMG_MAX_MB
	imgMu          sync.Mutex
	imgIDValido    = regexp.MustCompile(`^[0-9a-f]{32}$`)
	errImgInvalida = errors.New("imagem inválida (use PNG, JPEG ou GIF)")
	errImgGrande   = fmt.Errorf("imagem maior que %d MB", imgMaxBytes>>20)
	errImgCheio    = errors.New("armazenamento de imagens compartilhadas cheio; tente mais tarde")
	errImgExpirada = errors.New("imagem não encontrada ou expirada (links com imagem valem 7 dias)")
)

// extensões guardadas por formato (a extensão também define o Content-Type ao servir)
var imgExt = map[string]string{"png": "png", "jpeg": "jpg", "gif": "gif"}
var imgMime = map[string]string{"png": "image/png", "jpg": "image/jpeg", "gif": "image/gif"}

func imgDir() string { return filepath.Join(dataDir, "imagens") }

// base64Imagem extrai os bytes de um base64 com ou sem o prefixo data:image/...;base64,
func base64Imagem(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "base64,"); i >= 0 {
		s = s[i+len("base64,"):]
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("imagem: base64 inválido: %w", err)
	}
	return raw, nil
}

// idDaImagem é o hash do conteúdo: mesma imagem, mesmo id.
func idDaImagem(raw []byte) string {
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:16])
}

// salvarImagem valida e guarda a imagem (ou renova o prazo se já existe).
func salvarImagem(raw []byte) (id string, expira time.Time, renovada bool, err error) {
	if len(raw) == 0 || len(raw) > imgMaxBytes {
		if len(raw) > imgMaxBytes {
			return "", time.Time{}, false, errImgGrande
		}
		return "", time.Time{}, false, errImgInvalida
	}
	cfg, formato, derr := image.DecodeConfig(bytes.NewReader(raw))
	ext, ok := imgExt[formato]
	if derr != nil || !ok || cfg.Width*cfg.Height > maxPixels {
		return "", time.Time{}, false, errImgInvalida
	}
	id = idDaImagem(raw)

	imgMu.Lock()
	defer imgMu.Unlock()
	if err = os.MkdirAll(imgDir(), 0o755); err != nil {
		return "", time.Time{}, false, err
	}
	destino := filepath.Join(imgDir(), id+"."+ext)
	agora := time.Now()
	if _, serr := os.Stat(destino); serr == nil { // já existe: renova o prazo
		if err = os.Chtimes(destino, agora, agora); err != nil {
			return "", time.Time{}, false, err
		}
		return id, agora.Add(imgTTL), true, nil
	}
	// só guarda se couber no limite total (descarta antes o que já venceu)
	if tamanhoImagens()+int64(len(raw)) > imgMaxTotal {
		limparImagensExpiradasLocked()
		if tamanhoImagens()+int64(len(raw)) > imgMaxTotal {
			return "", time.Time{}, false, errImgCheio
		}
	}
	tmp, err := os.CreateTemp(imgDir(), ".envio-*")
	if err != nil {
		return "", time.Time{}, false, err
	}
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return "", time.Time{}, false, fmt.Errorf("gravando imagem: %v %v", werr, cerr)
	}
	if err = os.Rename(tmp.Name(), destino); err != nil { // rename é atômico: ninguém lê arquivo pela metade
		os.Remove(tmp.Name())
		return "", time.Time{}, false, err
	}
	return id, agora.Add(imgTTL), false, nil
}

// lerImagem devolve os bytes, o Content-Type e quando expira. Arquivo vencido não é servido.
func lerImagem(id string) (raw []byte, mime string, expira time.Time, err error) {
	if !imgIDValido.MatchString(id) { // também impede path traversal
		return nil, "", time.Time{}, errImgExpirada
	}
	imgMu.Lock()
	defer imgMu.Unlock()
	for ext, m := range imgMime {
		caminho := filepath.Join(imgDir(), id+"."+ext)
		st, serr := os.Stat(caminho)
		if serr != nil {
			continue
		}
		if time.Since(st.ModTime()) > imgTTL {
			os.Remove(caminho)
			return nil, "", time.Time{}, errImgExpirada
		}
		raw, err = os.ReadFile(caminho)
		return raw, m, st.ModTime().Add(imgTTL), err
	}
	return nil, "", time.Time{}, errImgExpirada
}

// tamanhoImagens soma os bytes das imagens guardadas (chamar com imgMu travado).
func tamanhoImagens() (total int64) {
	entradas, _ := os.ReadDir(imgDir())
	for _, e := range entradas {
		if info, err := e.Info(); err == nil && !e.IsDir() {
			total += info.Size()
		}
	}
	return total
}

// limparImagensExpiradas apaga o que passou do prazo e devolve quantos arquivos removeu.
func limparImagensExpiradas() int {
	imgMu.Lock()
	defer imgMu.Unlock()
	return limparImagensExpiradasLocked()
}

func limparImagensExpiradasLocked() int {
	entradas, err := os.ReadDir(imgDir())
	if err != nil {
		return 0
	}
	removidas := 0
	for _, e := range entradas {
		info, err := e.Info()
		if err != nil || e.IsDir() {
			continue
		}
		// vencidas e também sobras de envios interrompidos (.envio-*) com mais de 1 h
		limite := imgTTL
		if strings.HasPrefix(e.Name(), ".envio-") {
			limite = time.Hour
		}
		if time.Since(info.ModTime()) > limite {
			if os.Remove(filepath.Join(imgDir(), e.Name())) == nil {
				removidas++
			}
		}
	}
	return removidas
}

// iniciarLimpezaImagens roda a limpeza agora e depois a cada 24 h.
func iniciarLimpezaImagens() {
	go func() {
		for {
			if n := limparImagensExpiradas(); n > 0 {
				log.Printf("[IMAGENS] limpeza: %d arquivo(s) vencido(s) removido(s)", n)
			}
			time.Sleep(imgLimpezaIntervalo)
		}
	}()
}

// handleImagens atende POST /imagens (salva ou renova) e GET /imagens/{id} (devolve).
func handleImagens(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/imagens"), "/")
	if id == "" {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "use POST")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, imgMaxBytes*4/3+4096) // base64 incha ~33%
		var in struct {
			Imagem string `json:"imagem"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			code := 400
			var mb *http.MaxBytesError
			if errors.As(err, &mb) {
				code = http.StatusRequestEntityTooLarge
			}
			writeErr(w, code, fmt.Sprintf("JSON inválido ou grande demais: %v", err))
			return
		}
		raw, err := base64Imagem(in.Imagem)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		id, expira, renovada, err := salvarImagem(raw)
		switch {
		case errors.Is(err, errImgGrande):
			writeErr(w, http.StatusRequestEntityTooLarge, err.Error())
		case errors.Is(err, errImgInvalida):
			writeErr(w, 400, err.Error())
		case errors.Is(err, errImgCheio):
			writeErr(w, http.StatusInsufficientStorage, err.Error())
		case err != nil:
			writeErr(w, 500, fmt.Sprintf("erro ao salvar a imagem: %v", err))
		default:
			log.Printf("[IMAGENS] %s salva (renovada=%v, %d bytes), expira em %s", id, renovada, len(raw), expira.Format(time.RFC3339))
			writeJSON(w, 200, map[string]any{
				"ok": true, "id": id, "url": "/imagens/" + id, "renovada": renovada,
				"validade_dias": int(imgTTL / (24 * time.Hour)),
				"expira_em":     expira.UTC().Format(time.RFC3339),
			})
		}
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeErr(w, http.StatusMethodNotAllowed, "use GET")
		return
	}
	raw, mime, expira, err := lerImagem(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store") // o prazo pode ser renovado ou vencer
	w.Header().Set("X-Expira-Em", expira.UTC().Format(time.RFC3339))
	_, _ = w.Write(raw)
}
