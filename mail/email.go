package mail

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
)

type EmailConfig struct {
	SMTPHost     string
	SMTPPort     string
	SMTPUsername string
	SMTPPassword string
	FromEmail    string
	FromName     string
}

type MailSender interface {
	SendOTP(to, otp, schoolCode string) error
}

type EmailService struct {
	config EmailConfig
}

func NewEmailService(config *EmailConfig) *EmailService {
	return &EmailService{config: *config}
}

func (es *EmailService) SendOTP(to, otp, schoolCode string) error {
	subject := fmt.Sprintf("Exun Registration Permanent Authentication Code - %s", otp)

	htmlBody, err := es.renderOTPTemplate(otp, schoolCode)
	if err != nil {
		return fmt.Errorf("failed to render email template: %v", err)
	}

	return es.sendEmail(to, subject, htmlBody)
}

func (es *EmailService) SendEmail(to, subject, htmlBody string) error {
	return es.sendEmail(to, subject, htmlBody)
}

func (es *EmailService) renderOTPTemplate(otp string, schoolCode string) (string, error) {
	templatePath := filepath.Join("mail", "otp.html")

	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to read template file: %v", err)
	}

	tmpl, err := template.New("otp").Parse(string(templateContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %v", err)
	}

	td := `<td align="center" valign="middle" style="width:48px; height:48px; background-color:#2977f5; color:#ffffff; border-radius:12px; font-size:1.5rem; font-weight:700; line-height:48px; text-align:center; vertical-align:middle; padding:0;">%s</td>`
	var cells strings.Builder
	for _, d := range strings.Split(otp, "") {
		cells.WriteString(fmt.Sprintf(td, template.HTMLEscapeString(d)))
	}

	data := struct {
		OTP        string
		OTPCells   template.HTML
		SchoolCode string
	}{OTP: otp, OTPCells: template.HTML(cells.String()), SchoolCode: schoolCode}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %v", err)
	}
	return buf.String(), nil
}

func (es *EmailService) sendEmail(to, subject, htmlBody string) error {
	if es.config.SMTPUsername == "" || es.config.SMTPPassword == "" {
		return fmt.Errorf("SMTP credentials not configured")
	}

	auth := smtp.PlainAuth("", es.config.SMTPUsername, es.config.SMTPPassword, es.config.SMTPHost)

	headers := map[string]string{
		"From":         fmt.Sprintf("%s <%s>", es.config.FromName, es.config.FromEmail),
		"To":           to,
		"Subject":      subject,
		"MIME-Version": "1.0",
		"Content-Type": "text/html; charset=UTF-8",
	}

	message := ""
	for k, v := range headers {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + htmlBody

	addr := fmt.Sprintf("%s:%s", es.config.SMTPHost, es.config.SMTPPort)
	if err := smtp.SendMail(addr, auth, es.config.FromEmail, []string{to}, []byte(message)); err != nil {
		return fmt.Errorf("failed to send email: %v", err)
	}

	log.Printf("OTP email sent successfully to %s", to)
	return nil
}
