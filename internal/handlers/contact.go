package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

type ContactRequest struct {
	Name              string `json:"name"`
	Email             string `json:"email"`
	Phone             string `json:"phone"`
	Message           string `json:"message"`
	Website           string `json:"website"`
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

func (h *Handler) HandleContact(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == h.Config.AllowedOrigin {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ContactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	// Honeypot check — silently accept
	if req.Website != "" {
		sendJSON(w, http.StatusOK, map[string]string{"message": "Message sent successfully"})
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "Name is required"})
		return
	}
	if strings.TrimSpace(req.Email) == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "Email is required"})
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "Message is required"})
		return
	}
	if len(req.Message) < 40 {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "Message must be at least 40 characters"})
		return
	}

	// Verify Turnstile
	if h.Config.TurnstileSecret != "" {
		verified, err := verifyTurnstile(req.TurnstileResponse, h.Config.TurnstileSecret)
		if err != nil {
			log.Printf("Turnstile verification error: %v", err)
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "Verification failed"})
			return
		}
		if !verified {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "Verification failed"})
			return
		}
	}

	if err := h.sendEmail(req); err != nil {
		log.Printf("Email send error: %v", err)
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to send message"})
		return
	}

	sendJSON(w, http.StatusOK, map[string]string{"message": "Message sent successfully"})
}

func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

func (h *Handler) sendEmail(req ContactRequest) error {
	if h.Config.PostmarkToken == "" || h.Config.FromEmail == "" || h.Config.ToEmail == "" {
		return fmt.Errorf("email configuration not set")
	}

	email := PostmarkEmail{
		From:    h.Config.FromEmail,
		To:      h.Config.ToEmail,
		Subject: fmt.Sprintf("Contact Form: Message from %s", req.Name),
		TextBody: fmt.Sprintf("New contact form submission:\n\nName: %s\nEmail: %s\nPhone: %s\n\nMessage:\n%s\n",
			req.Name, req.Email, req.Phone, req.Message),
		HtmlBody: fmt.Sprintf(`<h2>New Contact Form Submission</h2>
<p><strong>Name:</strong> %s</p>
<p><strong>Email:</strong> <a href="mailto:%s">%s</a></p>
<p><strong>Phone:</strong> %s</p>
<h3>Message:</h3>
<p>%s</p>`,
			req.Name, req.Email, req.Email, req.Phone,
			strings.ReplaceAll(req.Message, "\n", "<br>")),
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
	httpReq.Header.Set("X-Postmark-Server-Token", h.Config.PostmarkToken)

	resp, err := (&http.Client{}).Do(httpReq)
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

func sendJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
