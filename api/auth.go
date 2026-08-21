package api

import (
	crand "crypto/rand"
	"encoding/hex"
	"html/template"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/asd1asd00000/vpnshop/db"
)

var (
	sessionsMu sync.Mutex
	sessions   = map[string]time.Time{} // token → انقضا
)

func createSession() string {
	b := make([]byte, 32)
	if _, err := crand.Read(b); err != nil {
		return ""
	}
	token := hex.EncodeToString(b)
	sessionsMu.Lock()
	sessions[token] = time.Now().Add(24 * time.Hour)
	sessionsMu.Unlock()
	return token
}

func validSession(token string) bool {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	exp, ok := sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(sessions, token)
		return false
	}
	return true
}

func destroySession(token string) {
	sessionsMu.Lock()
	delete(sessions, token)
	sessionsMu.Unlock()
}

// checkCredentials اعتبارسنجی (اول config، بعد env)
func checkCredentials(user, pass string) bool {
	cfg := db.GetConfig()
	if cfg.Admin.Username != "" && cfg.Admin.Password != "" {
		return user == cfg.Admin.Username && pass == cfg.Admin.Password
	}
	adminUser := os.Getenv("ADMIN_USER")
	adminPass := os.Getenv("ADMIN_PASS")
	if adminUser == "" {
		adminUser = "admin"
	}
	if adminPass == "" {
		adminPass = "123456"
	}
	return user == adminUser && pass == adminPass
}

// LoginHandler صفحه ورود + پردازش فرم
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("admin_session"); err == nil && validSession(c.Value) {
		http.Redirect(w, r, AdminBasePath(), http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodPost {
		user := r.FormValue("username")
		pass := r.FormValue("password")
		if checkCredentials(user, pass) {
			http.SetCookie(w, &http.Cookie{
				Name:     "admin_session",
				Value:    createSession(),
				Path:     "/",
				HttpOnly: true,
				MaxAge:   86400,
			})
			db.LogEvent("general", "success", "🔐 ورود ادمین موفق")
			http.Redirect(w, r, AdminBasePath(), http.StatusSeeOther)
			return
		}
		db.LogEvent("general", "warning", "⚠️ تلاش ورود ناموفق به ادمین")
		renderLogin(w, true)
		return
	}

	renderLogin(w, false)
}

// LogoutHandler خروج از حساب
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("admin_session"); err == nil {
		destroySession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "admin_session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, AdminBasePath()+"/login", http.StatusSeeOther)
}

func renderLogin(w http.ResponseWriter, failed bool) {
	tmpl, err := template.ParseFiles("templates/login.html")
	if err != nil {
		http.Error(w, "خطا در بارگذاری قالب ورود", http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, map[string]interface{}{"Failed": failed})
}
