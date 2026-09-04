package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/asd1asd00000/vpnshop/db"
)

var shamsiMonths = []string{"فروردین", "اردیبهشت", "خرداد", "تیر", "مرداد", "شهریور", "مهر", "آبان", "آذر", "دی", "بهمن", "اسفند"}

func jalali(gy, gm, gd int) (int, int, int) {
	gdm := []int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	gy2 := gy
	if gm > 2 {
		gy2 = gy + 1
	}
	days := 355666 + 365*gy + (gy2+3)/4 - (gy2+99)/100 + (gy2+399)/400 + gd + gdm[gm-1]
	jy := -1595 + 33*(days/12053)
	days %= 12053
	jy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		jy += (days - 1) / 365
		days = (days - 1) % 365
	}
	var jm, jd int
	if days < 186 {
		jm = 1 + days/31
		jd = 1 + days%31
	} else {
		jm = 7 + (days-186)/30
		jd = 1 + (days-186)%30
	}
	return jy, jm, jd
}

func shamsiShort(t time.Time) string {
	jy, jm, jd := jalali(t.Year(), int(t.Month()), t.Day())
	_ = jy
	return fmt.Sprintf("%02d/%02d", jm, jd)
}

func shamsiMonth(t time.Time) string {
	jy, jm, _ := jalali(t.Year(), int(t.Month()), t.Day())
	return fmt.Sprintf("%s %d", shamsiMonths[jm-1], jy)
}

func weekStart(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	off := (int(d.Weekday()) + 1) % 7 // شنبه = شروع هفته
	return d.AddDate(0, 0, -off)
}

func parsePaidAt(s string) (time.Time, bool) {
	layouts := []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04:05Z", time.RFC3339}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func bucketKey(t time.Time, mode string) string {
	switch mode {
	case "hour":
		return t.Format("2006-01-02T15")
	case "week":
		return weekStart(t).Format("2006-01-02")
	case "month":
		return t.Format("2006-01")
	default:
		return t.Format("2006-01-02")
	}
}

// StatsHandler آمار فروش (شامل آرشیوشده‌ها) بر اساس paid_at
func StatsHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	rangeKey := r.URL.Query().Get("range")
	if rangeKey == "" {
		rangeKey = "r7"
	}

	nowT := db.TehranNow()
	startOfToday := time.Date(nowT.Year(), nowT.Month(), nowT.Day(), 0, 0, 0, 0, db.TehranTZ)

	var start, end time.Time
	mode := "day"

	switch rangeKey {
	case "today":
		start, end, mode = startOfToday, nowT, "hour"
	case "yesterday":
		start, end, mode = startOfToday.AddDate(0, 0, -1), startOfToday, "hour"
	case "d3":
		start, end = startOfToday.AddDate(0, 0, -2), nowT
	case "d4":
		start, end = startOfToday.AddDate(0, 0, -3), nowT
	case "d5":
		start, end = startOfToday.AddDate(0, 0, -4), nowT
	case "d6":
		start, end = startOfToday.AddDate(0, 0, -5), nowT
	case "d7", "r7":
		start, end = startOfToday.AddDate(0, 0, -6), nowT
	case "r30":
		start, end = startOfToday.AddDate(0, 0, -29), nowT
	case "r90":
		start, end, mode = weekStart(startOfToday.AddDate(0, 0, -89)), nowT, "week"
	case "m6":
		start, end, mode = startOfToday.AddDate(0, -5, 0), nowT, "month"
	case "y1":
		start, end, mode = startOfToday.AddDate(0, -11, 0), nowT, "month"
	case "all":
		start, end, mode = time.Time{}, nowT, "month"
	default:
		start, end = startOfToday.AddDate(0, 0, -6), nowT
	}

	rows, err := db.DB.Query(`SELECT unique_amount, IFNULL(paid_at, '') FROM orders WHERE status = 'paid' AND paid_at IS NOT NULL`)
	if err != nil {
		http.Error(w, "خطا در خواندن آمار", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	sum := map[string]int64{}
	cnt := map[string]int{}
	var total int64
	var count int
	earliest := time.Time{}

	for rows.Next() {
		var amount int
		var paidAt string
		if err := rows.Scan(&amount, &paidAt); err != nil {
			continue
		}
		ut, ok := parsePaidAt(paidAt)
		if !ok {
			continue
		}
		tt := ut.In(db.TehranTZ)

		if rangeKey == "all" {
			if earliest.IsZero() || tt.Before(earliest) {
				earliest = tt
			}
		} else {
			if tt.Before(start) || tt.After(end) {
				continue
			}
		}

		k := bucketKey(tt, mode)
		sum[k] += int64(amount)
		cnt[k]++
		total += int64(amount)
		count++
	}

	if rangeKey == "all" {
		if earliest.IsZero() {
			start = startOfToday
		} else {
			start = time.Date(earliest.Year(), earliest.Month(), 1, 0, 0, 0, 0, db.TehranTZ)
		}
	}

	// ساخت بازه‌های مرتب (شامل روزهای صفر)
	var labels []string
	var keys []string
	switch mode {
	case "hour":
		for t := start; !t.After(end); t = t.Add(time.Hour) {
			keys = append(keys, t.Format("2006-01-02T15"))
			labels = append(labels, fmt.Sprintf("%02d:00", t.Hour()))
		}
	case "week":
		for t := weekStart(start); !t.After(end); t = t.AddDate(0, 0, 7) {
			keys = append(keys, t.Format("2006-01-02"))
			labels = append(labels, "هفته "+shamsiShort(t))
		}
	case "month":
		for t := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, db.TehranTZ); !t.After(end); t = t.AddDate(0, 1, 0) {
			keys = append(keys, t.Format("2006-01"))
			labels = append(labels, shamsiMonth(t))
		}
	default: // day
		for t := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, db.TehranTZ); !t.After(end); t = t.AddDate(0, 0, 1) {
			keys = append(keys, t.Format("2006-01-02"))
			labels = append(labels, shamsiShort(t))
		}
	}

	values := make([]int64, len(keys))
	counts := make([]int, len(keys))
	for i, k := range keys {
		values[i] = sum[k]
		counts[i] = cnt[k]
	}

	avg := int64(0)
	if count > 0 {
		avg = total / int64(count)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total":  total,
		"count":  count,
		"avg":    avg,
		"labels": labels,
		"values": values,
		"counts": counts,
	})
}
