package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"exunreg25/db"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type AuthConfig struct {
	Salt         string
	CookieSecure bool
}
type AuthToken struct {
	Email     string    `json:"email"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}
type OTPRequest struct {
	Email      string `json:"email"`
	SchoolCode string `json:"school_code,omitempty"`
}
type OTPResponse struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}
type LoginRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}
type AuthHandler struct {
	db         *db.Database
	config     *AuthConfig
	mailSender MailSender
}
type MailSender interface {
	SendOTP(to, otp, schoolCode string) error
}

func NewAuthHandler(db *db.Database, config *AuthConfig, mailSender MailSender) *AuthHandler {
	return &AuthHandler{
		db:         db,
		config:     config,
		mailSender: mailSender,
	}
}

func (ah *AuthHandler) generateAuthToken(email string) string {
	data := email + ah.config.Salt
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func (ah *AuthHandler) hashPassword(pw string) string {
	h := sha256.Sum256([]byte(ah.config.Salt + pw))
	return hex.EncodeToString(h[:])
}

func (ah *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !ah.isAuthenticated(r) {
		response := Response{Status: "error", Error: "Authentication required"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(response)
		return
	}
	email := ah.getAuthenticatedUser(r)
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := Response{Status: "error", Error: "Invalid request body"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}
	if len(req.New) < 8 {
		response := Response{Status: "error", Error: "Password must be at least 8 characters"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}
	userIface, err := ah.db.Get("users", email)
	if err != nil || userIface == nil {
		response := Response{Status: "error", Error: "User not found"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}
	u := userIface.(*db.User)
	if u.PasswordHash != "" {
		if ah.hashPassword(req.Current) != u.PasswordHash {
			response := Response{Status: "error", Error: "Current password incorrect"}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(response)
			return
		}
	}
	u.PasswordHash = ah.hashPassword(req.New)
	u.UpdatedAt = time.Now()
	if err := ah.db.Update("users", email, u); err != nil {
		response := Response{Status: "error", Error: "Failed to update password"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}
	response := Response{Status: "success", Message: "Password updated"}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ah *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Email       string `json:"email"`
		OTP         string `json:"otp"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := Response{Status: "error", Error: "Invalid request body"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}
	if !isValidEmail(req.Email) {
		response := Response{Status: "error", Error: "Invalid email"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}
	expected := ah.generateOTP(req.Email)
	if req.OTP != expected {
		response := Response{Status: "error", Error: "Invalid OTP"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(response)
		return
	}
	if len(req.NewPassword) < 8 {
		response := Response{Status: "error", Error: "Password must be at least 8 characters"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}
	userIface, err := ah.db.Get("users", req.Email)
	if err != nil || userIface == nil {
		user := &db.User{
			Username:      req.Email,
			Email:         req.Email,
			PasswordHash:  ah.hashPassword(req.NewPassword),
			Registrations: make(map[string][]db.Participant),
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		if cerr := ah.db.Create("users", user); cerr != nil {
			response := Response{Status: "error", Error: "Failed to create user"}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(response)
			return
		}
		response := Response{Status: "success", Message: "Password reset successful"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}
	u := userIface.(*db.User)
	u.PasswordHash = ah.hashPassword(req.NewPassword)
	u.UpdatedAt = time.Now()
	if err := ah.db.Update("users", req.Email, u); err != nil {
		response := Response{Status: "error", Error: "Failed to update password"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}
	response := Response{Status: "success", Message: "Password reset successful"}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ah *AuthHandler) generateOTP(email string) string {
	token := ah.generateAuthToken(email)
	last6 := token[len(token)-6:]
	otpInt, _ := strconv.ParseInt(last6, 16, 64)
	otpInt = otpInt % 1000000
	return fmt.Sprintf("%06d", otpInt)
}

func (ah *AuthHandler) validateAuthToken(email, token string) bool {
	expectedToken := ah.generateAuthToken(email)
	return token == expectedToken
}

func (ah *AuthHandler) isAuthenticated(r *http.Request) bool {
	emailCookie, err := r.Cookie("email")
	if err != nil {
		return false
	}
	tokenCookie, err := r.Cookie("auth_token")
	if err != nil {
		return false
	}
	return ah.validateAuthToken(emailCookie.Value, tokenCookie.Value)
}
func (ah *AuthHandler) getAuthenticatedUser(r *http.Request) string {
	emailCookie, err := r.Cookie("email")
	if err != nil {
		return ""
	}
	return emailCookie.Value
}

func (ah *AuthHandler) SendOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req OTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := Response{
			Status: "error",
			Error:  "Invalid request body",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}
	if !isValidEmail(req.Email) {
		response := Response{
			Status: "error",
			Error:  "Invalid email format",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}
	if u, gerr := ah.db.Get("users", req.Email); gerr == nil && u != nil {
		response := Response{
			Status: "error",
			Error:  "User already exists; request OTP only for new registrations or use login",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	} else {
		otp := ah.generateOTP(req.Email)

		if err := ah.mailSender.SendOTP(req.Email, otp, otp); err != nil {
			fmt.Printf("SendOTP error: %v\n", err)
			response := Response{
				Status: "error",
				Error:  err.Error(),
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(response)
			return
		}

		response := Response{
			Status:  "success",
			Message: "OTP sent successfully",
			Data: OTPResponse{
				Email: req.Email,
				OTP:   otp,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}
}

func (ah *AuthHandler) SendResetOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req OTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := Response{Status: "error", Error: "Invalid request body"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}
	if !isValidEmail(req.Email) {
		response := Response{Status: "error", Error: "Invalid email format"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}
	if u, gerr := ah.db.Get("users", req.Email); gerr != nil || u == nil {
		response := Response{Status: "error", Error: "User not found"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	} else {
		otp := ah.generateOTP(req.Email)

		if err := ah.mailSender.SendOTP(req.Email, otp, otp); err != nil {
			fmt.Printf("SendResetOTP error: %v\n", err)
			response := Response{Status: "error", Error: err.Error()}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(response)
			return
		}

		response := Response{Status: "success", Message: "OTP sent successfully", Data: OTPResponse{Email: req.Email, OTP: otp}}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}
}

func (ah *AuthHandler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := Response{
			Status: "error",
			Error:  "Invalid request body",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	expectedOTP := ah.generateOTP(req.Email)
	if req.OTP != expectedOTP {
		response := Response{
			Status: "error",
			Error:  "Invalid OTP",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(response)
		return
	}

	authToken := ah.generateAuthToken(req.Email)
	http.SetCookie(w, &http.Cookie{
		Name:     "email",
		Value:    req.Email,
		Path:     "/",
		HttpOnly: true,
		Secure:   ah.config.CookieSecure,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    authToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   ah.config.CookieSecure,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})

	user, err := ah.db.Get("users", req.Email)
	needsComplete := false
	if err != nil || user == nil {
		needsComplete = true
	} else {
		u := user.(*db.User)
		if strings.TrimSpace(u.Username) == "" || strings.TrimSpace(u.Fullname) == "" || len(strings.TrimSpace(u.PhoneNumber)) != 10 {
			needsComplete = true
		} else if !u.Individual && strings.TrimSpace(u.PrincipalsEmail) == "" {
			needsComplete = true
		}
	}

	response := Response{
		Status:  "success",
		Message: "OTP verified",
		Data: map[string]interface{}{
			"email":          req.Email,
			"needs_complete": needsComplete,
			"needs_signup":   needsComplete,
			"token":          authToken,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ah *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "email",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Expires:  time.Now().Add(-1 * time.Hour),
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Expires:  time.Now().Add(-1 * time.Hour),
	})

	response := Response{
		Status:  "success",
		Message: "Logout successful",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ah *AuthHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !ah.isAuthenticated(r) {
		response := Response{
			Status: "error",
			Error:  "Authentication required",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(response)
		return
	}
	email := ah.getAuthenticatedUser(r)

	userData, err := ah.db.Get("users", email)
	if err != nil {
		response := Response{
			Status: "error",
			Error:  "User not found",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}
	response := Response{
		Status:  "success",
		Message: "Profile retrieved successfully",
		Data:    userData,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ah *AuthHandler) CompleteSignup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method", http.StatusMethodNotAllowed)
		return
	}
	if !ah.isAuthenticated(r) {
		response := Response{
			Status: "error",
			Error:  "Authentication required",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(response)
		return
	}
	email := ah.getAuthenticatedUser(r)

	var signupData struct {
		Username string `json:"username"`
		Password string `json:"password,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&signupData); err != nil {
		response := Response{
			Status: "error",
			Error:  "Invalid request body",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if signupData.Password == "" {
		response := Response{
			Status: "error",
			Error:  "Password is required",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if len(signupData.Password) < 8 {
		response := Response{
			Status: "error",
			Error:  "Password must be at least 8 characters",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	existing, err := ah.db.Get("users", email)
	if err != nil {
		user := &db.User{
			Username:      signupData.Username,
			Email:         email,
			PasswordHash:  ah.hashPassword(signupData.Password),
			Registrations: make(map[string][]db.Participant),
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		if err := ah.db.Create("users", user); err != nil {
			fmt.Printf("Create user error: %v\n", err)
			response := Response{
				Status: "error",
				Error:  fmt.Sprintf("Failed to create user: %v", err),
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(response)
			return
		}
		fmt.Printf("User created: %s (%s)\n", user.Username, user.Email)
		response := Response{
			Status:  "success",
			Message: "Signup completed successfully",
			Data:    user,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}

	u := existing.(*db.User)
	modified := false
	if signupData.Username != "" {
		u.Username = signupData.Username
		modified = true
	}
	if signupData.Password != "" {
		u.PasswordHash = ah.hashPassword(signupData.Password)
		modified = true
	}
	if modified {
		u.UpdatedAt = time.Now()
		if err := ah.db.Update("users", email, u); err != nil {
			fmt.Printf("Update user error: %v\n", err)
			response := Response{
				Status: "error",
				Error:  fmt.Sprintf("Failed to update user: %v", err),
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(response)
			return
		}
	}

	fmt.Printf("User completed signup: %s (%s)\n", u.Username, u.Email)
	response := Response{
		Status:  "success",
		Message: "Signup completed successfully",
		Data:    u,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func isValidEmail(email string) bool {
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		return false
	}
	if len(email) < 5 {
		return false
	}
	for _, char := range email {
		if char < 32 || char > 126 {
			return false
		}
	}

	return true
}

func (ah *AuthHandler) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ah.isAuthenticated(r) {
			response := Response{
				Status: "error",
				Error:  "Authentication required",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(response)
			return
		}
		next.ServeHTTP(w, r)
	}
}

var globalAuthHandler *AuthHandler

func SetGlobalAuthHandler(handler *AuthHandler) {
	globalAuthHandler = handler
}

func SendOTP(w http.ResponseWriter, r *http.Request) {
	if globalAuthHandler == nil {
		http.Error(w, "Auth handler not initialized", http.StatusInternalServerError)
		return
	}
	globalAuthHandler.SendOTP(w, r)
}

func SendResetOTP(w http.ResponseWriter, r *http.Request) {
	if globalAuthHandler == nil {
		http.Error(w, "Auth handler not initialized", http.StatusInternalServerError)
		return
	}
	globalAuthHandler.SendResetOTP(w, r)
}

func ChangePassword(w http.ResponseWriter, r *http.Request) {
	if globalAuthHandler == nil {
		http.Error(w, "Auth handler not initialized", http.StatusInternalServerError)
		return
	}
	globalAuthHandler.ChangePassword(w, r)
}

func ResetPassword(w http.ResponseWriter, r *http.Request) {
	if globalAuthHandler == nil {
		http.Error(w, "Auth handler not initialized", http.StatusInternalServerError)
		return
	}
	globalAuthHandler.ResetPassword(w, r)
}

func VerifyOTP(w http.ResponseWriter, r *http.Request) {
	if globalAuthHandler == nil {
		http.Error(w, "Auth handler not initialized", http.StatusInternalServerError)
		return
	}
	globalAuthHandler.VerifyOTP(w, r)
}

func Logout(w http.ResponseWriter, r *http.Request) {
	if globalAuthHandler == nil {
		http.Error(w, "Auth handler not initialized", http.StatusInternalServerError)
		return
	}
	globalAuthHandler.Logout(w, r)
}

func CompleteSignup(w http.ResponseWriter, r *http.Request) {
	if globalAuthHandler == nil {
		http.Error(w, "Auth handler not initialized", http.StatusInternalServerError)
		return
	}
	globalAuthHandler.CompleteSignup(w, r)
}

func GetProfile(w http.ResponseWriter, r *http.Request) {
	if globalAuthHandler == nil {
		http.Error(w, "Auth handler not initialized", http.StatusInternalServerError)
		return
	}
	globalAuthHandler.GetProfile(w, r)
}
