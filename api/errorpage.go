package api

import (
	"net/http"
	"strconv"
	"strings"
)

// renderErrorPage صفحه خطای زیبا با رفرش خودکار و دستی نمایش می‌ده
// retryURL: آدرسی که دوباره تلاش می‌شه (معمولاً همون صفحه)
// autoRefreshSec: فاصله رفرش خودکار به ثانیه (0 = بدون رفرش خودکار)
func renderErrorPage(w http.ResponseWriter, title, message, retryURL string, autoRefreshSec int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)

	secStr := strconv.Itoa(autoRefreshSec)

	page := `<!DOCTYPE html>
<html lang="fa" dir="rtl">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>مشکلی پیش اومد | VPNShop</title>
<script src="https://cdn.tailwindcss.com"></script>
<style>
@import url('https://cdn.jsdelivr.net/gh/rastikerdar/vazirmatn@v33.003/Vazirmatn-font-face.css');
body { font-family: 'Vazirmatn', sans-serif; }
@keyframes floaty { 0%,100%{transform:translateY(0)} 50%{transform:translateY(-10px)} }
.floaty { animation: floaty 3s ease-in-out infinite; }
@keyframes spin-slow { to { transform: rotate(360deg); } }
.spin-slow { animation: spin-slow 2s linear infinite; }
</style>
</head>
<body class="bg-gradient-to-br from-gray-100 to-gray-200 min-h-screen flex items-center justify-center p-4">

<div class="max-w-md w-full bg-white rounded-2xl shadow-xl p-8 text-center">

    <!-- آیکون -->
    <div class="floaty w-20 h-20 bg-orange-100 rounded-full flex items-center justify-center mx-auto mb-6">
        <span class="text-4xl">⚠️</span>
    </div>

    <h1 class="text-xl md:text-2xl font-bold text-gray-800 mb-3">{{TITLE}}</h1>
    <p class="text-sm text-gray-600 leading-relaxed mb-6">{{MESSAGE}}</p>

    <!-- باکس رفرش خودکار -->
    <div id="autoBox" class="bg-blue-50 border border-blue-200 rounded-xl p-4 mb-6">
        <div class="flex items-center justify-center gap-2 text-blue-800 text-sm font-bold mb-1">
            <svg class="spin-slow w-4 h-4 text-blue-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/>
            </svg>
            <span>در حال تلاش خودکار دوباره...</span>
        </div>
        <p class="text-xs text-blue-600">تا <span id="cd" class="font-bold text-base">{{SEC}}</span> ثانیه دیگر صفحه دوباره بارگذاری می‌شه</p>
    </div>

    <!-- دکمه‌ها -->
    <div class="flex flex-col gap-3">
        <button onclick="goNow()" class="w-full bg-blue-600 hover:bg-blue-700 text-white font-bold py-3 rounded-xl transition">
            🔄 همین حالا دوباره تلاش کن
        </button>
        <button id="stopBtn" onclick="stopAuto()" class="w-full bg-gray-100 hover:bg-gray-200 text-gray-600 font-bold py-2.5 rounded-xl transition text-sm">
            ⏸ توقف تلاش خودکار
        </button>
        <a href="/" class="w-full bg-gray-600 hover:bg-gray-700 text-white font-bold py-3 rounded-xl transition text-sm">
            🏠 بازگشت به صفحه اصلی فروشگاه
        </a>
    </div>

    <p class="text-[10px] text-gray-400 mt-6">
        اگه این مشکل ادامه داشت، چند دقیقه بعد دوباره تلاش کنید یا به پشتیبانی اطلاع بدید.
    </p>

</div>

<script>
    const RETRY = '{{RETRY}}';
    let sec = {{SECN}};
    let timer = null;

    function toFa(n) {
        return String(n).replace(/\d/g, d => '۰۱۲۳۴۵۶۷۸۹'[d]);
    }

    function goNow() {
        window.location.href = RETRY;
    }

    function stopAuto() {
        if (timer) clearInterval(timer);
        document.getElementById('autoBox').classList.add('hidden');
        document.getElementById('stopBtn').classList.add('hidden');
    }

    if (sec > 0) {
        document.getElementById('cd').textContent = toFa(sec);
        timer = setInterval(() => {
            sec--;
            if (sec <= 0) {
                clearInterval(timer);
                goNow();
            } else {
                document.getElementById('cd').textContent = toFa(sec);
            }
        }, 1000);
    } else {
        stopAuto();
    }
</script>

</body>
</html>`

	r := strings.NewReplacer(
		"{{TITLE}}", title,
		"{{MESSAGE}}", message,
		"{{RETRY}}", retryURL,
		"{{SEC}}", secStr,
		"{{SECN}}", secStr,
	)
	w.Write([]byte(r.Replace(page)))
}
