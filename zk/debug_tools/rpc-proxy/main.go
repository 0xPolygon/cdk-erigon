package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"math/rand"
	"net/http"
	"time"
)

const (
	backendURL    = "http://localhost:8545" // Ethereum node
	delayProb     = 0.1                     // simulate 10% probability delay
	delayDuration = 60 * time.Second        // delay for 60 seconds
	listenAddr    = ":8080"
)

type RPCRequest struct {
	Method string `json:"method"`
}

func main() {
	http.HandleFunc("/", handleRPC)
	log.Printf("RPC proxy listening on %s\n", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, nil))
}

func handleRPC(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received RPC request: %s %s", r.Method, r.URL.Path)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("Request content: %s", string(body))

	var rpcReq RPCRequest
	if err = json.Unmarshal(body, &rpcReq); err != nil {
		http.Error(w, "Invalid JSON-RPC", http.StatusBadRequest)
		return
	}

	if rpcReq.Method == "eth_getLogs" && rand.Float64() < delayProb {
		log.Println("Simulating delay for eth_getLogs")
		time.Sleep(delayDuration)
	}

	req, err := http.NewRequest(r.Method, backendURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	req.Header = r.Header
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "identity")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to forward request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	log.Printf("Response content: %s", string(respBody))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, bytes.NewReader(respBody))
}
