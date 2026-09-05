package api

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/asd1asd00000/vpnshop/db"
)

// SendEmailBackup ارسال بکاپ به ایمیل
func SendEmailBackup(config db.EmailBackupConfig) error {
	if !config.Enabled || config.Email == "" || config.SMTPServer == "" {
		return fmt.Errorf("تنظیمات ایمیل ناقص است")
	}

	// ساخت بکاپ موقت
	timestamp := time.Now().Format("20060102_150405")
	tmpDB := fmt.Sprintf("/tmp/vpnshop_email_backup_%s.db", timestamp)
	zipPath := fmt.Sprintf("/tmp/vpnshop_email_backup_%s.zip", timestamp)

	defer os.Remove(tmpDB)
	defer os.Remove(zipPath)

	if _, err := db.DB.Exec(fmt.Sprintf("VACUUM INTO '%s'", tmpDB)); err != nil {
		return fmt.Errorf("خطا در گرفتن بکاپ: %v", err)
	}

	if err := createBackupZipForEmail(zipPath, tmpDB); err != nil {
		return fmt.Errorf("خطا در ساخت zip: %v", err)
	}

	// خواندن فایل zip
	zipData, err := os.ReadFile(zipPath)
	if err != nil {
		return fmt.Errorf("خطا در خواندن zip: %v", err)
	}

	// ساخت ایمیل با پیوست
	subject := fmt.Sprintf("📦 بکاپ VPNShop - %s", timestamp)
	body := fmt.Sprintf("بکاپ کامل دیتابیس و تنظیمات VPNShop\nتاریخ: %s\n\nاین فایل شامل:\n- vpnshop.db (دیتابیس)\n- config.json (تنظیمات)", timestamp)

	msg := buildEmailWithAttachment(config.Email, subject, body, fmt.Sprintf("vpnshop_backup_%s.zip", timestamp), zipData)

	// ارسال با SMTP
	auth := smtp.PlainAuth("", config.SMTPUser, config.SMTPPass, config.SMTPServer)
	addr := fmt.Sprintf("%s:%d", config.SMTPServer, config.SMTPPort)

	if err := smtp.SendMail(addr, auth, config.SMTPUser, []string{config.Email}, msg); err != nil {
		return fmt.Errorf("خطا در ارسال ایمیل: %v", err)
	}

	log.Printf("✅ بکاپ به ایمیل %s ارسال شد", config.Email)
	return nil
}

// SendTestEmail ارسال ایمیل تست
func SendTestEmail(config db.EmailBackupConfig) error {
	if config.Email == "" || config.SMTPServer == "" {
		return fmt.Errorf("تنظیمات ایمیل ناقص است")
	}

	subject := "✅ تست ایمیل VPNShop"
	body := "این یک ایمیل تست است.\nاگر این ایمیل را دریافت کردید، تنظیمات SMTP شما درست است."

	msg := buildSimpleEmail(config.Email, subject, body)

	auth := smtp.PlainAuth("", config.SMTPUser, config.SMTPPass, config.SMTPServer)
	addr := fmt.Sprintf("%s:%d", config.SMTPServer, config.SMTPPort)

	return smtp.SendMail(addr, auth, config.SMTPUser, []string{config.Email}, msg)
}

func buildSimpleEmail(to, subject, body string) []byte {
	return []byte(fmt.Sprintf("From: VPNShop Backup\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", to, subject, body))
}

func buildEmailWithAttachment(to, subject, body, filename string, attachment []byte) []byte {
	boundary := "BOUNDARY" + fmt.Sprintf("%d", time.Now().UnixNano())

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: VPNShop Backup\r\n"))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary))
	msg.WriteString("\r\n")

	// متن
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)
	msg.WriteString("\r\n\r\n")

	// پیوست
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString(fmt.Sprintf("Content-Type: application/zip; name=\"%s\"\r\n", filename))
	msg.WriteString(fmt.Sprintf("Content-Transfer-Encoding: base64\r\n"))
	msg.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", filename))
	msg.WriteString("\r\n")
	msg.WriteString(base64Encode(attachment))
	msg.WriteString("\r\n")

	msg.WriteString(fmt.Sprintf("--%s--", boundary))

	return []byte(msg.String())
}

func base64Encode(data []byte) string {
	const base64Chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var result strings.Builder
	for i := 0; i < len(data); i += 3 {
		var b uint32
		remaining := len(data) - i
		if remaining >= 3 {
			b = uint32(data[i])<<16 | uint32(data[i+1])<<8 | uint32(data[i+2])
			result.WriteByte(base64Chars[(b>>18)&0x3F])
			result.WriteByte(base64Chars[(b>>12)&0x3F])
			result.WriteByte(base64Chars[(b>>6)&0x3F])
			result.WriteByte(base64Chars[b&0x3F])
		} else if remaining == 2 {
			b = uint32(data[i])<<16 | uint32(data[i+1])<<8
			result.WriteByte(base64Chars[(b>>18)&0x3F])
			result.WriteByte(base64Chars[(b>>12)&0x3F])
			result.WriteByte(base64Chars[(b>>6)&0x3F])
			result.WriteByte('=')
		} else if remaining == 1 {
			b = uint32(data[i]) << 16
			result.WriteByte(base64Chars[(b>>18)&0x3F])
			result.WriteByte(base64Chars[(b>>12)&0x3F])
			result.WriteByte('=')
			result.WriteByte('=')
		}
	}
	return result.String()
}

func createBackupZipForEmail(zipPath, dbPath string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zw := zip.NewWriter(zipFile)
	defer zw.Close()

	if err := addFileToZipForEmail(zw, dbPath, "vpnshop.db"); err != nil {
		return err
	}

	if _, err := os.Stat("./config.json"); err == nil {
		if err := addFileToZipForEmail(zw, "./config.json", "config.json"); err != nil {
			return err
		}
	}

	return nil
}

func addFileToZipForEmail(zw *zip.Writer, srcPath, nameInZip string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	w, err := zw.Create(nameInZip)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, src)
	return err
}

// StartAutoEmailBackup شروع تایمر بکاپ خودکار روزانه
func StartAutoEmailBackup() {
	go func() {
		// صبر ۵ دقیقه بعد از شروع سرور
		time.Sleep(5 * time.Minute)

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			cfg := db.GetConfig()
			if cfg.EmailBackup.Enabled {
				log.Printf("📧 ارسال بکاپ خودکار روزانه به %s", cfg.EmailBackup.Email)
				if err := SendEmailBackup(cfg.EmailBackup); err != nil {
					db.LogEventf("general", "error", "❌ خطا در بکاپ خودکار: %v", err)
				} else {
					db.LogEvent("general", "success", "✅ بکاپ خودکار روزانه با موفقیت ارسال شد")
				}
			}
		}
	}()

	log.Println("📧 تایمر بکاپ خودکار روزانه شروع شد (اولین ارسال: فردا)")
}
