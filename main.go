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

	"github.com/curtisbraxdale/chirpy/internal/auth"
	"github.com/curtisbraxdale/chirpy/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	platform := os.Getenv("PLATFORM")
	secret := os.Getenv("TOKEN_SECRET")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Printf("Error connecting to database: %s", err)
	}
	dbQueries := database.New(db)

	serveMux := http.NewServeMux()
	apiCfg := apiConfig{queries: dbQueries, platform: platform, secret: secret}
	fileHandler := http.StripPrefix("/app/", http.FileServer(http.Dir(".")))

	serveMux.Handle("/app/", apiCfg.middlewareMetricsInc(fileHandler))
	serveMux.HandleFunc("GET /api/healthz", readiHandler)
	serveMux.HandleFunc("GET /admin/metrics", apiCfg.hitsHandler)
	serveMux.HandleFunc("POST /admin/reset", apiCfg.resetHandler)
	serveMux.HandleFunc("POST /api/users", apiCfg.createUserHandler)
	serveMux.HandleFunc("POST /api/chirps", apiCfg.createChirpHandler)
	serveMux.HandleFunc("GET /api/chirps", apiCfg.getChirpsHandler)
	serveMux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.getChirpHandler)
	serveMux.HandleFunc("DELETE /api/chirps/{chirpID}", apiCfg.delChirpHandler)
	serveMux.HandleFunc("POST /api/login", apiCfg.loginHandler)
	serveMux.HandleFunc("POST /api/refresh", apiCfg.refreshHandler)
	serveMux.HandleFunc("POST /api/revoke", apiCfg.revokeHandler)
	serveMux.HandleFunc("PUT /api/users", apiCfg.updateUserHandler)

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
	secret         string
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
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		return
	}
	hashedPassword, err := auth.HashPassword(params.Password)
	if err != nil {
		log.Printf("Error hashing password: %s", err)
		w.WriteHeader(500)
		return
	}
	dbUserParams := database.CreateUserParams{Email: params.Email, HashedPassword: hashedPassword}
	dbUser, err := cfg.queries.CreateUser(context.Background(), dbUserParams)
	if err != nil {
		log.Printf("Error creating user: %s", err)
		w.WriteHeader(500)
		return
	}
	newUser := User{ID: dbUser.ID, CreatedAt: dbUser.CreatedAt, UpdatedAt: dbUser.UpdatedAt, Email: dbUser.Email}
	respondWithJSON(w, 201, newUser)
}

type User struct {
	ID           uuid.UUID `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Email        string    `json:"email"`
	Token        string    `json:"token"`
	RefreshToken string    `json:"refresh_token"`
}

func (cfg *apiConfig) createChirpHandler(w http.ResponseWriter, req *http.Request) {
	type parameters struct {
		Body string `json:"body"`
	}
	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		return
	}
	// Checking User Tokens
	token, err := auth.GetBearerToken(req.Header)
	if err != nil {
		log.Printf("Error getting bearer token: %s", err)
		w.WriteHeader(401)
		return
	}
	validUserID, err := auth.ValidateJWT(token, cfg.secret)
	if err != nil {
		log.Printf("Error validating jwt: %s", err)
		w.WriteHeader(401)
		return
	}
	// Validate & Censor Chirp
	if len(params.Body) > 140 {
		respondWithError(w, 400, "Chirp is too long")
	} else {
		cleanedBody := cleanChirp(params.Body)
		chirpParams := database.CreateChirpParams{Body: cleanedBody, UserID: validUserID}
		dbChirp, err := cfg.queries.CreateChirp(context.Background(), chirpParams)
		if err != nil {
			log.Printf("Error creating user: %s", err)
			w.WriteHeader(500)
			return
		}
		newChirp := Chirp{ID: dbChirp.ID, CreatedAt: dbChirp.CreatedAt, UpdatedAt: dbChirp.UpdatedAt, Body: dbChirp.Body, UserID: dbChirp.UserID}
		respondWithJSON(w, 201, newChirp)
	}
}

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}

func (cfg *apiConfig) getChirpsHandler(w http.ResponseWriter, req *http.Request) {
	dbChirps, err := cfg.queries.GetChirps(context.Background())
	if err != nil {
		log.Printf("Error getting chirps: %s", err)
		w.WriteHeader(500)
		return
	}
	chirps := []Chirp{}
	for _, c := range dbChirps {
		chirps = append(chirps, Chirp{ID: c.ID, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, Body: c.Body, UserID: c.UserID})
	}
	respondWithJSON(w, 200, chirps)
}

func (cfg *apiConfig) getChirpHandler(w http.ResponseWriter, req *http.Request) {
	chirpID, err := uuid.Parse(req.PathValue("chirpID"))
	if err != nil {
		log.Printf("Error parsing uuid: %s", err)
		w.WriteHeader(500)
		return
	}
	dbChirp, err := cfg.queries.GetChirp(context.Background(), chirpID)
	if err != nil {
		log.Printf("Chirp not found: %s", err)
		w.WriteHeader(404)
		return
	}
	chirp := Chirp{ID: dbChirp.ID, CreatedAt: dbChirp.CreatedAt, UpdatedAt: dbChirp.UpdatedAt, Body: dbChirp.Body, UserID: dbChirp.UserID}
	respondWithJSON(w, 200, chirp)
}

func (cfg *apiConfig) loginHandler(w http.ResponseWriter, req *http.Request) {
	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		return
	}
	dbUser, err := cfg.queries.GetUserByEmail(context.Background(), params.Email)
	if err != nil {
		log.Print("Incorrect email or password")
		w.WriteHeader(401)
		return
	}
	err = auth.CheckPasswordHash(dbUser.HashedPassword, params.Password)
	if err != nil {
		log.Print("Incorrect email or password")
		w.WriteHeader(401)
		return
	}
	// Create JWT token.
	token := ""
	token, err = auth.MakeJWT(dbUser.ID, cfg.secret, time.Hour)
	if err != nil {
		log.Printf("Error creating JWT: %s", err)
		w.WriteHeader(500)
	}
	// Create refresh token.
	refreshToken, err := auth.MakeRefreshToken()
	if err != nil {
		log.Printf("Error creating refresh token: %s", err)
		w.WriteHeader(500)
	}
	// Store refresh token in database.
	refTokenParams := database.CreateRefreshTokenParams{Token: refreshToken, ExpiresAt: sql.NullTime{Time: time.Now().Add(time.Hour * 24 * 60), Valid: true}, UserID: dbUser.ID, RevokedAt: sql.NullTime{Valid: false}}
	dbRefToken, err := cfg.queries.CreateRefreshToken(context.Background(), refTokenParams)
	if err != nil {
		log.Printf("Error storing refresh token: %s", err)
		w.WriteHeader(500)
	}

	user := User{ID: dbUser.ID, CreatedAt: dbUser.CreatedAt, UpdatedAt: dbUser.UpdatedAt, Email: dbUser.Email, Token: token, RefreshToken: dbRefToken.Token}
	respondWithJSON(w, 200, user)
}

func (cfg *apiConfig) refreshHandler(w http.ResponseWriter, req *http.Request) {
	type TokenString struct {
		Token string `json:"token"`
	}
	// Get refresh token from header.
	refToken, err := auth.GetBearerToken(req.Header)
	if err != nil {
		log.Printf("Error getting refresh token: %s", err)
		w.WriteHeader(401)
		return
	}

	// Get associated user from database.
	dbRefToken, err := cfg.queries.GetUserByToken(context.Background(), refToken)
	if err != nil {
		log.Printf("Invalid refresh token: %s", err)
		w.WriteHeader(401)
		return
	}
	// Check if token has been revoked.
	if dbRefToken.RevokedAt.Valid || !dbRefToken.ExpiresAt.Valid || dbRefToken.ExpiresAt.Time.Before(time.Now()) {
		w.WriteHeader(401)
		return
	}
	// Create new JWT that expires in 1 hour.
	token := ""
	token, err = auth.MakeJWT(dbRefToken.UserID, cfg.secret, time.Hour)
	if err != nil {
		log.Printf("Error creating JWT: %s", err)
		w.WriteHeader(500)
		return
	}

	respBody := TokenString{Token: token}
	respondWithJSON(w, 200, respBody)
}

func (cfg *apiConfig) revokeHandler(w http.ResponseWriter, req *http.Request) {
	// Get refresh token from header.
	refToken, err := auth.GetBearerToken(req.Header)
	if err != nil {
		log.Printf("Error getting refresh token: %s", err)
		w.WriteHeader(401)
		return
	}
	err = cfg.queries.RevokeToken(context.Background(), refToken)
	if err != nil {
		log.Printf("Error revoking refresh token: %s", err)
		w.WriteHeader(401)
		return
	}
	w.WriteHeader(204)
	return
}

func (cfg *apiConfig) updateUserHandler(w http.ResponseWriter, req *http.Request) {
	// Get refresh token from header.
	token, err := auth.GetBearerToken(req.Header)
	if err != nil {
		log.Printf("Error getting access token: %s", err)
		w.WriteHeader(401)
		return
	}

	// Use refresh token to get user by ID.
	userID, err := auth.ValidateJWT(token, cfg.secret)
	if err != nil {
		log.Print("Invalid token.")
		w.WriteHeader(401)
		return
	}
	dbUser, err := cfg.queries.GetUserByID(context.Background(), userID)
	if err != nil {
		log.Printf("Error getting user by ID: %s", err)
		w.WriteHeader(401)
		return
	}

	// Get new email and password from request body.
	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err = decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		return
	}

	// Hash new password.
	hashedPass, err := auth.HashPassword(params.Password)
	if err != nil {
		log.Printf("Error hashing password: %s", err)
		w.WriteHeader(500)
		return
	}

	updateParams := database.UpdateEmailPassParams{ID: dbUser.ID, Email: params.Email, HashedPassword: hashedPass}
	err = cfg.queries.UpdateEmailPass(context.Background(), updateParams)
	if err != nil {
		log.Print("Error updating email & password.")
		w.WriteHeader(500)
		return
	}

	// Get User from database, with changes.
	dbUser, err = cfg.queries.GetUserByID(context.Background(), dbUser.ID)
	if err != nil {
		log.Print("Wrong userID.")
		w.WriteHeader(401)
		return
	}
	user := User{ID: dbUser.ID, CreatedAt: dbUser.CreatedAt, UpdatedAt: dbUser.UpdatedAt, Email: dbUser.Email}
	respondWithJSON(w, 200, user)
}

func (cfg *apiConfig) delChirpHandler(w http.ResponseWriter, req *http.Request) {
	// Get refresh token from header.
	token, err := auth.GetBearerToken(req.Header)
	if err != nil {
		log.Printf("Error getting access token: %s", err)
		w.WriteHeader(401)
		return
	}

	// Use refresh token to get user by ID.
	userID, err := auth.ValidateJWT(token, cfg.secret)
	if err != nil {
		log.Print("Invalid token.")
		w.WriteHeader(401)
		return
	}

	// Get Chirp ID from request path.
	chirpID, err := uuid.Parse(req.PathValue("chirpID"))
	if err != nil {
		log.Printf("Error parsing uuid: %s", err)
		w.WriteHeader(400)
		return
	}
	// Get Chirp from database.
	dbChirp, err := cfg.queries.GetChirp(context.Background(), chirpID)
	if err != nil {
		log.Print("Chirp not found.")
		w.WriteHeader(404)
		return
	}

	// Ensure UserID == dbCHirp.UserID.
	if userID != dbChirp.UserID {
		log.Print("Invalid User.")
		w.WriteHeader(403)
		return
	}
	err = cfg.queries.DeleteChirp(context.Background(), chirpID)
	if err != nil {
		log.Print("Chirp not found.")
		w.WriteHeader(404)
		return
	}
	w.WriteHeader(204)
}
