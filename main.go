package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/curtisbraxdale/chirpy/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	platform := os.Getenv("PLATFORM")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Printf("Error connecting to database: %s", err)
	}
	dbQueries := database.New(db)

	serveMux := http.NewServeMux()
	apiCfg := apiConfig{queries: dbQueries, platform: platform}
	fileHandler := http.StripPrefix("/app/", http.FileServer(http.Dir(".")))

	serveMux.Handle("/app/", apiCfg.middlewareMetricsInc(fileHandler))
	serveMux.HandleFunc("GET /api/healthz", readiHandler)
	serveMux.HandleFunc("POST /api/validate_chirp", validateHandler)
	serveMux.HandleFunc("GET /admin/metrics", apiCfg.hitsHandler)
	serveMux.HandleFunc("POST /admin/reset", apiCfg.resetHandler)
	serveMux.HandleFunc("POST /api/users", apiCfg.createUserHandler)

	server := http.Server{}
	server.Handler = serveMux
	server.Addr = ":8080"

	log.Fatal(server.ListenAndServe())
}

func readiHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Add("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(200)
	w.Write([]byte("OK"))
}

func validateHandler(w http.ResponseWriter, req *http.Request) {
	type parameters struct {
		Body string `json:"body"`
	}
	type cleanedValues struct {
		CleanedBody string `json:"cleaned_body"`
	}

	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		return
	}

	if len(params.Body) > 140 {
		respondWithError(w, 400, "Chirp is too long")
	} else {
		cleanedBody := cleanChirp(params.Body)
		respBody := cleanedValues{CleanedBody: cleanedBody}
		respondWithJSON(w, 200, respBody)
	}
}

func (apiCfg *apiConfig) hitsHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Add("Content-Type", "text/html")
	w.WriteHeader(200)
	hitString := fmt.Sprintf("<html><body><h1>Welcome, Chirpy Admin</h1><p>Chirpy has been visited %d times!</p></body></html>", apiCfg.fileserverHits.Load())
	w.Write([]byte(hitString))
}

func (apiCfg *apiConfig) resetHandler(w http.ResponseWriter, req *http.Request) {
	if apiCfg.platform != "dev" {
		w.WriteHeader(403)
	} else {
		apiCfg.fileserverHits.Store(0)
		err := apiCfg.queries.DeleteUsers(context.Background())
		if err != nil {
			log.Printf("Error deleting users: %s", err)
			w.WriteHeader(500)
			return
		}
		w.Header().Add("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(200)
		w.Write([]byte("Deleted Users & Hits Reset."))
	}
}

type apiConfig struct {
	fileserverHits atomic.Int32
	queries        *database.Queries
	platform       string
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func respondWithError(w http.ResponseWriter, code int, msg string) {
	type errorValues struct {
		Error string `json:"error"`
	}
	respBody := errorValues{Error: msg}
	dat, err := json.Marshal(respBody)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(dat)
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	dat, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(dat)
}

func cleanChirp(body string) string {
	splitWords := strings.Split(body, " ")
	for i, word := range splitWords {
		if strings.ToLower(word) == "kerfuffle" || strings.ToLower(word) == "sharbert" || strings.ToLower(word) == "fornax" {
			splitWords[i] = "****"
		}
	}
	cleanedBody := strings.Join(splitWords, " ")
	return cleanedBody
}

func (cfg *apiConfig) createUserHandler(w http.ResponseWriter, req *http.Request) {
	type parameters struct {
		Email string `json:"email"`
	}
	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		return
	}
	dbUser, err := cfg.queries.CreateUser(context.Background(), params.Email)
	if err != nil {
		log.Printf("Error creating user: %s", err)
		w.WriteHeader(500)
		return
	}
	newUser := User{ID: dbUser.ID, CreatedAt: dbUser.CreatedAt, UpdatedAt: dbUser.UpdatedAt, Email: dbUser.Email}
	respondWithJSON(w, 201, newUser)
}

type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

func (cfg *apiConfig) createChirpHandler(w http.ResponseWriter, req *http.Request) {
	type params struct {
		Body   string    `json:"body"`
		UserID uuid.UUID `json:"user_id"`
	}
}
