package api

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"

	enczip "github.com/alexmullins/zip"

	"github.com/asd1asd00000/vpnshop/db"
)

// 🎯 ساخت نام فایل بر اساس روز، ماه، سال، ساعت و دقیقه شمسی
func generateJalaliFilename() string {
	now := db.TehranNow()
	jy, jm, jd := jalali(now.Year(), int(now.Month()), now.Day())
	// فرمت: روز-ماه-سال_ساعت-دقیقه (مثلاً 10-08-1402_15-45)
	return fmt.Sprintf("%02d-%02d-%04d_%02d-%02d", jd, jm, jy, now.Hour(), now.Minute())
}

// ───────────── بکاپ ایمیل ─────────────

func getFromAddress(config db.EmailBackupConfig) string {
	if strings.Contains(strings.ToLower(config.SMTPServer), "resend") {
		return "onboarding@resend.dev"
	}
	return config.SMTPUser
}

func SendEmailBackup(config db.EmailBackupConfig) error {
	if config.Email == "" || config.SMTPServer == "" {
		return fmt.Errorf("تنظیمات ایمیل ناقص است")
	}

	filename := "vpnshop_" + generateJalaliFilename() + ".zip"
	tmpDB := fmt.Sprintf("/tmp/vpnshop_email_%d.db", time.Now().Unix())
	zipPath := fmt.Sprintf("/tmp/%s", filename)

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

	subject := "📦 بکاپ VPNShop"
	body := fmt.Sprintf("بکاپ کامل دیتابیس و تنظیمات\nتاریخ و زمان: %s\n\nشامل فایل‌ها:\n- vpnshop.db\n- config.json", generateJalaliFilename())
	if config.ZipPassword != "" {
		body += "\n\n🔒 این فایل با رمز عبور محافظت شده است."
	}

	from := getFromAddress(config)
	msg := buildEmailWithAttachment(from, config.Email, subject, body, filename, zipData)

	auth := smtp.PlainAuth("", config.SMTPUser, config.SMTPPass, config.SMTPServer)
	addr := fmt.Sprintf("%s:%d", config.SMTPServer, config.SMTPPort)

	if err := smtp.SendMail(addr, auth, from, []string{config.Email}, msg); err != nil {
		return fmt.Errorf("خطا در ارسال ایمیل: %v", err)
	}

	log.Printf("✅ بکاپ به ایمیل %s ارسال شد", config.Email)
	return nil
}

// ───────────── بکاپ تلگرام ─────────────

func SendTelegramBackup(config db.TelegramBackupConfig) error {
	if config.BotToken == "" || config.ChatID == "" {
		return fmt.Errorf("تنظیمات تلگرام ناقص است")
	}

	filename := "vpnshop_" + generateJalaliFilename() + ".zip"
	tmpDB := fmt.Sprintf("/tmp/vpnshop_tg_%d.db", time.Now().Unix())
	zipPath := fmt.Sprintf("/tmp/%s", filename)

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

	caption := fmt.Sprintf("📦 **بکاپ خودکار VPNShop**\n\n📅 زمان: `%s`\n\nشامل: `vpnshop.db` و `config.json`", generateJalaliFilename())
	if config.ZipPassword != "" {
		caption += "\n\n🔒 فایل زیپ دارای رمز است."
	}

	return sendDocumentToTelegram(config.BotToken, config.ChatID, zipPath, filename, caption)
}

func sendDocumentToTelegram(token, chatID, filePath, filename, caption string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("chat_id", chatID)
	writer.WriteField("caption", caption)
	writer.WriteField("parse_mode", "Markdown")

	part, err := writer.CreateFormFile("document", filename)
	if err != nil {
		return err
	}
	io.Copy(part, file)
	writer.Close()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", token)
	req, _ := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("وضعیت %d، پاسخ تلگرام: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ───────────── تایمر مشترک خودکار ─────────────

// StartAutoBackups تایمر بکاپ خودکار تلگرام و ایمیل
func StartAutoBackups() {
	go func() {
		var lastEmailRun, lastTgRun time.Time
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			cfg := db.GetConfig()
			now := time.Now()

			// ایمیل
			if cfg.EmailBackup.Enabled && cfg.EmailBackup.Email != "" {
				h := cfg.EmailBackup.IntervalHours
				if h <= 0 {
					h = 24
				}
				if lastEmailRun.IsZero() || now.Sub(lastEmailRun) >= time.Duration(h)*time.Hour {
					if lastEmailRun.IsZero() {
						lastEmailRun = now
					} else {
						lastEmailRun = now
						log.Printf("📧 در حال ارسال بکاپ خودکار به ایمیل...")
						err := SendEmailBackup(cfg.EmailBackup)
						logBackupAutoStatus("ایمیل", err)
					}
				}
			}

			// تلگرام
			if cfg.TelegramBackup.Enabled && cfg.TelegramBackup.BotToken != "" {
				h := cfg.TelegramBackup.IntervalHours
				if h <= 0 {
					h = 24
				}
				if lastTgRun.IsZero() || now.Sub(lastTgRun) >= time.Duration(h)*time.Hour {
					if lastTgRun.IsZero() {
						lastTgRun = now
					} else {
						lastTgRun = now
						log.Printf("✈️ در حال ارسال بکاپ خودکار به تلگرام...")
						err := SendTelegramBackup(cfg.TelegramBackup)
						logBackupAutoStatus("تلگرام", err)
					}
				}
			}
		}
	}()
	log.Println("🔄 سیستم بکاپ خودکار (ایمیل و تلگرام) فعال شد")
}

func logBackupAutoStatus(dest string, err error) {
	success := err == nil
	msg := fmt.Sprintf("ارسال بکاپ خودکار به %s", dest)
	if !success {
		msg += fmt.Sprintf(" ناموفق بود: %v", err)
		db.LogEventf("general", "error", "❌ %s", msg)
	} else {
		msg += " با موفقیت انجام شد."
		db.LogEvent("general", "success", "✅ "+msg)
	}
	db.AddBackupLog(db.BackupLog{
		Time:    db.FormatTehranUTC(time.Now().UTC().Format(time.RFC3339)),
		Success: success,
		Details: msg,
	})
}

// ───────────── توابع فشرده‌سازی و زیپ ─────────────

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

		header := &enczip.FileHeader{
			Name:   item.name,
			Method: enczip.Deflate,
		}
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

func buildEmailWithAttachment(from, to, subject, body, filename string, attachment []byte) []byte {
	boundary := "BOUNDARY" + fmt.Sprintf("%d", time.Now().UnixNano())
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: VPNShop Backup <%s>\r\n", from))
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
