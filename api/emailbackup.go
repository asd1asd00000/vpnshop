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

	enczip "github.com/alexmullins/zip"

	"github.com/asd1asd00000/vpnshop/db"
)

// SendEmailBackup ارسال بکاپ به ایمیل (با زیپ رمزدار اختیاری)
func SendEmailBackup(config db.EmailBackupConfig) error {
	if config.Email == "" || config.SMTPServer == "" {
		return fmt.Errorf("تنظیمات ایمیل ناقص است")
	}

	timestamp := time.Now().Format("20060102_150405")
	tmpDB := fmt.Sprintf("/tmp/vpnshop_email_backup_%s.db", timestamp)
	zipPath := fmt.Sprintf("/tmp/vpnshop_email_backup_%s.zip", timestamp)

	defer os.Remove(tmpDB)
	defer os.Remove(zipPath)

	if _, err := db.DB.Exec(fmt.Sprintf("VACUUM INTO '%s'", tmpDB)); err != nil {
		return fmt.Errorf("خطا در گرفتن بکاپ: %v", err)
	}

	var err error
	if config.ZipPassword != "" {
		err = createEncryptedBackupZip(zipPath, tmpDB, config.ZipPassword)
	} else {
		err = createBackupZipForEmail(zipPath, tmpDB)
	}
	if err != nil {
		return fmt.Errorf("خطا در ساخت zip: %v", err)
	}

	zipData, err := os.ReadFile(zipPath)
	if err != nil {
		return fmt.Errorf("خطا در خواندن zip: %v", err)
	}

	subject := fmt.Sprintf("📦 بکاپ VPNShop - %s", timestamp)
	body := fmt.Sprintf("بکاپ کامل دیتابیس و تنظیمات VPNShop\nتاریخ: %s\n\nشامل:\n- vpnshop.db\n- config.json", timestamp)
	if config.ZipPassword != "" {
		body += "\n\n🔒 رمز فایل زیپ: " + config.ZipPassword
	}

	msg := buildEmailWithAttachment(config.Email, subject, body, fmt.Sprintf("vpnshop_backup_%s.zip", timestamp), zipData)

	auth := smtp.PlainAuth("", config.SMTPUser, config.SMTPPass, config.SMTPServer)
	addr := fmt.Sprintf("%s:%d", config.SMTPServer, config.SMTPPort)

	if err := smtp.SendMail(addr, auth, config.SMTPUser, []string{config.Email}, msg); err != nil {
		return fmt.Errorf("خطا در ارسال ایمیل: %v", err)
	}

	log.Printf("✅ بکاپ به ایمیل %s ارسال شد", config.Email)
	return nil
}

// createEncryptedBackupZip ساخت زیپ رمزدار
func createEncryptedBackupZip(zipPath, dbPath, password string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := enczip.NewWriter(f)
	defer zw.Close()

	files := []struct{ src, name string }{
		{dbPath, "vpnshop.db"},
		{"./config.json", "config.json"},
	}

	for _, item := range files {
		if _, err := os.Stat(item.src); err != nil {
			continue
		}
		header := &enczip.FileHeader{Name: item.name, Method: enczip.Deflate, Flags: enczip.AESEncryption}
		header.SetPassword(password)
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		src, err := os.Open(item.src)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, src); err != nil {
			src.Close()
			return err
		}
		src.Close()
	}
	return nil
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
	msg.WriteString("From: VPNShop Backup\r\n")
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary))
	msg.WriteString("\r\n")
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	msg.WriteString(body)
	msg.WriteString("\r\n\r\n")
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString(fmt.Sprintf("Content-Type: application/zip; name=\"%s\"\r\n", filename))
	msg.WriteString("Content-Transfer-Encoding: base64\r\n")
	msg.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", filename))
	msg.WriteString("\r\n")
	msg.WriteString(base64Encode(attachment))
	msg.WriteString("\r\n")
	msg.WriteString(fmt.Sprintf("--%s--", boundary))
	return []byte(msg.String())
}

func base64Encode(data []byte) string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var result strings.Builder
	for i := 0; i < len(data); i += 3 {
		var b uint32
		remaining := len(data) - i
		if remaining >= 3 {
			b = uint32(data[i])<<16 | uint32(data[i+1])<<8 | uint32(data[i+2])
			result.WriteByte(chars[(b>>18)&0x3F])
			result.WriteByte(chars[(b>>12)&0x3F])
			result.WriteByte(chars[(b>>6)&0x3F])
			result.WriteByte(chars[b&0x3F])
		} else if remaining == 2 {
			b = uint32(data[i])<<16 | uint32(data[i+1])<<8
			result.WriteByte(chars[(b>>18)&0x3F])
			result.WriteByte(chars[(b>>12)&0x3F])
			result.WriteByte(chars[(b>>6)&0x3F])
			result.WriteByte('=')
		} else if remaining == 1 {
			b = uint32(data[i]) << 16
			result.WriteByte(chars[(b>>18)&0x3F])
			result.WriteByte(chars[(b>>12)&0x3F])
			result.WriteByte('=')
			result.WriteByte('=')
		}
	}
	return result.String()
}

// StartAutoEmailBackup تایمر بکاپ خودکار با بازه قابل تنظیم (ساعت)
func StartAutoEmailBackup() {
	go func() {
		lastRun := time.Now()
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			cfg := db.GetConfig()
			if !cfg.EmailBackup.Enabled || cfg.EmailBackup.Email == "" {
				continue
			}
			hours := cfg.EmailBackup.IntervalHours
			if hours <= 0 {
				hours = 24
			}
			if time.Since(lastRun) >= time.Duration(hours)*time.Hour {
				lastRun = time.Now()
				log.Printf("📧 ارسال بکاپ خودکار به %s (بازه: %d ساعت)", cfg.EmailBackup.Email, hours)
				if err := SendEmailBackup(cfg.EmailBackup); err != nil {
					db.LogEventf("general", "error", "❌ خطا در بکاپ خودکار: %v", err)
				} else {
					db.LogEvent("general", "success", "✅ بکاپ خودکار با موفقیت ارسال شد")
				}
			}
		}
	}()
	log.Println("📧 تایمر بکاپ خودکار ایمیل فعال شد")
}
