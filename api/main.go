package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

type ContactRequest struct {
	Name              string `json:"name"`
	Email             string `json:"email"`
	Phone             string `json:"phone"`
	Message           string `json:"message"`
	Website           string `json:"website"` // Honeypot field
	TurnstileResponse string `json:"cf-turnstile-response"`
}

type TurnstileVerifyResponse struct {
	Success bool `json:"success"`
}

type PostmarkEmail struct {
	From     string `json:"From"`
	To       string `json:"To"`
	Subject  string `json:"Subject"`
	TextBody string `json:"TextBody"`
	HtmlBody string `json:"HtmlBody"`
}

func main() {
	port := os.Getenv("API_PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/api/contact", corsMiddleware(handleContact))
	http.HandleFunc("/api/health", handleHealth)

	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		allowedOrigin := os.Getenv("ALLOWED_ORIGIN")
		if allowedOrigin == "" {
			allowedOrigin = "http://localhost:1313"
		}

		origin := r.Header.Get("Origin")
		if origin == allowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleContact(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ContactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Check honeypot - silently accept to not tip off bots
	if req.Website != "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "Message sent successfully"})
		return
	}

	// Validate required fields
	if strings.TrimSpace(req.Name) == "" {
		sendError(w, "Name is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Email) == "" {
		sendError(w, "Email is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		sendError(w, "Message is required", http.StatusBadRequest)
		return
	}
	if len(req.Message) < 40 {
		sendError(w, "Message must be at least 40 characters", http.StatusBadRequest)
		return
	}

	// Verify Turnstile token
	turnstileSecret := os.Getenv("TURNSTILE_SECRET")
	if turnstileSecret != "" {
		verified, err := verifyTurnstile(req.TurnstileResponse, turnstileSecret)
		if err != nil {
			log.Printf("Turnstile verification error: %v", err)
			sendError(w, "Verification failed", http.StatusInternalServerError)
			return
		}
		if !verified {
			sendError(w, "Verification failed", http.StatusBadRequest)
			return
		}
	}

	// Send email via Postmark
	if err := sendEmail(req); err != nil {
		log.Printf("Email send error: %v", err)
		sendError(w, "Failed to send message", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Message sent successfully"})
}

func verifyTurnstile(token, secret string) (bool, error) {
	data := fmt.Sprintf("secret=%s&response=%s", secret, token)
	resp, err := http.Post(
		"https://challenges.cloudflare.com/turnstile/v0/siteverify",
		"application/x-www-form-urlencoded",
		strings.NewReader(data),
	)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	var result TurnstileVerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, err
	}

	return result.Success, nil
}

func sendEmail(req ContactRequest) error {
	postmarkToken := os.Getenv("POSTMARK_TOKEN")
	fromEmail := os.Getenv("FROM_EMAIL")
	toEmail := os.Getenv("TO_EMAIL")

	if postmarkToken == "" || fromEmail == "" || toEmail == "" {
		return fmt.Errorf("email configuration not set")
	}

	email := PostmarkEmail{
		From:    fromEmail,
		To:      toEmail,
		Subject: fmt.Sprintf("Contact Form: Message from %s", req.Name),
		TextBody: fmt.Sprintf(`New contact form submission:

Name: %s
Email: %s
Phone: %s

Message:
%s
`, req.Name, req.Email, req.Phone, req.Message),
		HtmlBody: fmt.Sprintf(`<h2>New Contact Form Submission</h2>
<p><strong>Name:</strong> %s</p>
<p><strong>Email:</strong> <a href="mailto:%s">%s</a></p>
<p><strong>Phone:</strong> %s</p>
<h3>Message:</h3>
<p>%s</p>
`, req.Name, req.Email, req.Email, req.Phone, strings.ReplaceAll(req.Message, "\n", "<br>")),
	}

	body, err := json.Marshal(email)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequest("POST", "https://api.postmarkapp.com/email", bytes.NewReader(body))
	if err != nil {
		return err
	}

	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Postmark-Server-Token", postmarkToken)

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("postmark error: %s", string(respBody))
	}

	return nil
}

func sendError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
