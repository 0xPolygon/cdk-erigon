package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"

	circuits "github.com/iden3/go-circuits/v2"
	auth "github.com/iden3/go-iden3-auth/v2"
	"github.com/iden3/go-iden3-auth/v2/loaders"
	"github.com/iden3/go-iden3-auth/v2/pubsignals"
	"github.com/iden3/go-iden3-auth/v2/state"
	"github.com/iden3/iden3comm/v2/protocol"
)

type session struct {
	ID           string                                `json:"id"`
	CallType     string                                `json:"callType"`
	SchemaType   string                                `json:"schemaType"`
	Args         map[string]interface{}                `json:"args"`
	Sender       string                                `json:"sender"`
	TokenAddress string                                `json:"tokenAddress"`
	CreatedAt    time.Time                             `json:"createdAt"`
	ExpiresAt    time.Time                             `json:"expiresAt"`
	Request      *protocol.AuthorizationRequestMessage `json:"-"`
	Verified     bool                                  `json:"verified"`
	JWT          string                                `json:"jwt,omitempty"`
}

type server struct {
	baseURL   string
	jwtSecret []byte
	reqID     int
	schemaCtx string
	mu        sync.RWMutex
	sessions  map[string]*session
	resolver  map[string]pubsignals.StateResolver
	verifier  *auth.Verifier
}

func newServer() *server {
	base := os.Getenv("PUBLIC_BASE_URL")
	if base == "" {
		log.Fatal("PUBLIC_BASE_URL not set")
	}
	jwtSecret := []byte(os.Getenv("JWT_SECRET"))
	if len(jwtSecret) == 0 {
		log.Fatal("JWT_SECRET not set")
	}
	reqID := 2
	if v := os.Getenv("REQUEST_ID"); v != "" {
		fmt.Sscanf(v, "%d", &reqID)
	}
	schemaCtx := os.Getenv("SCHEMA_CTX")
	if schemaCtx == "" {
		schemaCtx = "https://raw.githubusercontent.com/iden3/claim-schema-vocab/main/schemas/json-ld/non-zero-balance.jsonld"
	}

	resolver := os.Getenv("STATE_RESOLVER_URL")
	contract := os.Getenv("STATE_RESOLVER_CONTRACT")
	network := os.Getenv("STATE_RESOLVER_NETWORK")

	resolvers := map[string]pubsignals.StateResolver{
		"privado:main": state.NewETHResolver("https://rpc-mainnet.privado.id", "0x3C9acB2205Aa72A05F6D77d708b5Cf85FCa3a896"),
		"polygon:amoy": state.NewETHResolver("https://rpc-amoy.polygon.technology", "0x1a4cC30f2aA0377b0c3bc9848766D90cb4404124"),
		"zkevm:test":   state.NewETHResolver("https://rpc.cardona.zkevm-rpc.com", "0x3C9acB2205Aa72A05F6D77d708b5Cf85FCa3a896"),
	}

	if resolver != "" && contract != "" && network != "" {
		resolvers[network] = state.NewETHResolver(resolver, contract)
	} else {
		log.Fatalln("STATE_RESOLVER_URL, STATE_RESOLVER_CONTRACT and STATE_RESOLVER_NETWORK must be set")
	}

	vf, err := auth.NewVerifier(loaders.NewEmbeddedKeyLoader(), resolvers)
	if err != nil || vf == nil {
		log.Fatalf("failed to init verifier: %v", err)
	}
	return &server{
		baseURL:   strings.TrimRight(base, "/"),
		jwtSecret: jwtSecret,
		reqID:     reqID,
		schemaCtx: schemaCtx,
		sessions:  map[string]*session{},
		resolver:  resolvers,
		verifier:  vf,
	}
}

func (s *server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CallType   string                 `json:"callType"`
		SchemaType string                 `json:"schemaType"`
		Args       map[string]interface{} `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id := uuid.NewString()
	now := time.Now()
	exp := now.Add(10 * time.Minute)
	var sender, token string
	if v, ok := in.Args["from"].(string); ok {
		sender = v
	}
	if v, ok := in.Args["to"].(string); ok {
		token = v
	}
	sess := &session{ID: id, CallType: in.CallType, SchemaType: in.SchemaType, Args: in.Args, Sender: sender, TokenAddress: token, CreatedAt: now, ExpiresAt: exp}

	req := auth.CreateAuthorizationRequest("Prove balance > 0", "offchain-verifier", fmt.Sprintf("%s/callback?challengeId=%s", s.baseURL, id))
	var pr protocol.ZeroKnowledgeProofRequest
	pr.ID = uint32(s.reqID)
	pr.CircuitID = string(circuits.AtomicQuerySigV2CircuitID)
	q := map[string]interface{}{
		"allowedIssuers": []string{"*"},
		"credentialSubject": map[string]interface{}{
			"address": map[string]interface{}{"value": token},
			"balance": map[string]interface{}{"$gte": 1},
		},
		"context": s.schemaCtx,
		"type":    "Balance",
	}
	pr.Query = q
	req.Body.Scope = append(req.Body.Scope, pr)
	// Note: some wallets derive 'challenge' differently; for gating JWT only we omit explicit challenge here.
	sess.Request = &req

	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	out := map[string]interface{}{
		"url":           fmt.Sprintf("%s/requests/%s", s.baseURL, id),
		"challengeId":   id,
		"expiresAt":     exp.Unix(),
		"universalLink": fmt.Sprintf("https://wallet.privado.id#request_uri=%s", urlEncode(fmt.Sprintf("%s/requests/%s", s.baseURL, id))),
	}
	writeJSON(w, out)
}

func (s *server) handleGetRequest(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/requests/")
	s.mu.RLock()
	sess := s.sessions[id]
	s.mu.RUnlock()
	if sess == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, sess.Request)
}

func (s *server) handleCallback(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("challengeId")
	s.mu.RLock()
	sess := s.sessions[id]
	s.mu.RUnlock()
	if sess == nil {
		http.Error(w, "unknown challengeId", http.StatusBadRequest)
		return
	}
	tokenBytes, err := ioReadAllLimit(r.Body, 2<<20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ar := *sess.Request
	if _, err := s.verifier.FullVerify(r.Context(), string(tokenBytes), ar); err != nil {
		http.Error(w, fmt.Sprintf("verify failed: %v", err), http.StatusBadRequest)
		return
	}
	jwtToken, err := s.makeJWT()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	sess.Verified = true
	sess.JWT = jwtToken
	s.sessions[id] = sess
	s.mu.Unlock()
	writeJSON(w, map[string]string{"token": jwtToken})
}

func (s *server) makeJWT() (string, error) {
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(60 * time.Second)),
		Issuer:    "offchain-verifier",
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(s.jwtSecret)
}

func main() {
	srv := newServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/sessions", srv.handleCreateSession)
	// Poll session: GET /sessions/{id}
	mux.HandleFunc("/sessions/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/sessions/")
		srv.mu.RLock()
		sess := srv.sessions[id]
		srv.mu.RUnlock()
		if sess == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]interface{}{
			"verified":  sess.Verified,
			"token":     sess.JWT,
			"expiresAt": sess.ExpiresAt.Unix(),
		})
	})
	mux.HandleFunc("/requests/", srv.handleGetRequest)
	mux.HandleFunc("/callback", srv.handleCallback)
	addr := ":8789"
	if v := os.Getenv("PORT"); v != "" {
		addr = ":" + v
	}
	log.Printf("offchain verifier listening on %s\nbase=%s", addr, srv.baseURL)
	log.Fatal(http.ListenAndServe(addr, logMiddleware(mux)))
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("content-type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

func urlEncode(s string) string {
	r := strings.NewReplacer(":", "%3A", "/", "%2F", "?", "%3F", "=", "%3D", "#", "%23")
	return r.Replace(s)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func ioReadAllLimit(b io.Reader, n int64) ([]byte, error) { return io.ReadAll(io.LimitReader(b, n)) }
