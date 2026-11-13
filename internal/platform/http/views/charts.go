package views

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"hesapkitap/internal/money"
	"hesapkitap/internal/report"
)

// Grafikler sunucuda saf SVG olarak çizilir: dış kütüphane yok, CSP'ye uyar, yazdırılabilir.

var monthShort = [...]string{"Oca", "Şub", "Mar", "Nis", "May", "Haz", "Tem", "Ağu", "Eyl", "Eki", "Kas", "Ara"}
var monthLong = [...]string{"Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran", "Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık"}

func monthName(t time.Time) string { return monthLong[t.Month()-1] + " " + strconv.Itoa(t.Year()) }

func fnum(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

// compactTL writes kuruş as "12,5 B ₺" (bin) or "1,2 Mn ₺" (milyon) for axis labels.
func compactTL(kurus int64) string {
	v := float64(kurus) / 100
	trim := func(x float64) string {
		s := strings.Replace(strconv.FormatFloat(x, 'f', 1, 64), ".", ",", 1)
		return strings.TrimSuffix(s, ",0")
	}
	switch {
	case math.Abs(v) >= 1_000_000:
		return trim(v/1_000_000) + " Mn"
	case math.Abs(v) >= 1_000:
		return trim(v/1_000) + " B"
	}
	return strconv.FormatFloat(v, 'f', 0, 64)
}

// niceStep returns a round step (1, 2, 2.5, 5 × 10^k) so the axis has about `ticks` divisions.
func niceStep(maxVal float64, ticks int) float64 {
	if maxVal <= 0 {
		return 100 // 1 ₺
	}
	raw := maxVal / float64(ticks)
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if raw <= m*mag {
			return m * mag
		}
	}
	return 10 * mag
}

type bar struct {
	X, Y, W, H float64
	Title      string
	Collected  bool
}

type tick struct {
	Y     float64
	Label string
}

type xLabel struct {
	X    float64
	Text string
}

// BarChart is the geometry of the "fatura vs tahsilat" chart.
type BarChart struct {
	W, H           float64
	Left, Right    float64
	Bars           []bar
	Ticks          []tick
	Labels         []xLabel
	HasData        bool
	PlotBottom     float64
	PlotTopPadding float64
}

func NewBarChart(series []report.MonthPoint) BarChart {
	const (
		w, h             = 760.0, 300.0
		left, right      = 52.0, 8.0
		top, bottom      = 12.0, 34.0
		groupGap, barGap = 0.28, 3.0
	)
	var maxVal float64
	for _, p := range series {
		maxVal = math.Max(maxVal, math.Max(float64(p.Invoiced), float64(p.Collected)))
	}
	step := niceStep(maxVal, 4)
	axisMax := step * 4
	plotW, plotH := w-left-right, h-top-bottom
	c := BarChart{W: w, H: h, Left: left, Right: w - right, HasData: maxVal > 0, PlotBottom: h - bottom}
	for i := 0; i <= 4; i++ {
		v := step * float64(i)
		c.Ticks = append(c.Ticks, tick{Y: top + plotH - v/axisMax*plotH, Label: compactTL(int64(v))})
	}
	gw := plotW / float64(len(series))
	bw := (gw*(1-groupGap) - barGap) / 2
	for i, p := range series {
		x0 := left + gw*float64(i) + gw*groupGap/2
		label := monthShort[p.Month.Month()-1]
		if p.Month.Month() == time.January || i == 0 {
			label += " " + fmt.Sprintf("%02d", p.Month.Year()%100)
		}
		c.Labels = append(c.Labels, xLabel{X: left + gw*float64(i) + gw/2, Text: label})
		inv := float64(p.Invoiced) / axisMax * plotH
		col := float64(p.Collected) / axisMax * plotH
		c.Bars = append(c.Bars,
			bar{X: x0, Y: top + plotH - inv, W: bw, H: inv, Title: monthName(p.Month) + " — Kesilen: " + money.FormatTL(p.Invoiced)},
			bar{X: x0 + bw + barGap, Y: top + plotH - col, W: bw, H: col, Title: monthName(p.Month) + " — Tahsilat: " + money.FormatTL(p.Collected), Collected: true},
		)
	}
	return c
}

// Segment is one slice of the aging donut (circumference = 100).
type Segment struct {
	Dash   string
	Offset float64
	Color  string
	Label  string
	Value  int64
	Pct    int
}

// bucketColors follow the bucket order of report.BucketLabels: yeşil → sarı → turuncu → kırmızı → koyu kırmızı → gri.
var bucketColors = [...]string{"#10b981", "#fbbf24", "#f97316", "#f43f5e", "#9f1239", "#94a3b8"}

func bucketColor(i int) string { return bucketColors[i] }

func NewDonut(buckets [6]int64) (segs []Segment, total int64) {
	for _, v := range buckets {
		total += v
	}
	if total == 0 {
		return nil, 0
	}
	offset := 25.0 // 12 yönünden başla
	for i, v := range buckets {
		if v == 0 {
			continue
		}
		pct := float64(v) / float64(total) * 100
		segs = append(segs, Segment{
			Dash: fmt.Sprintf("%.2f %.2f", pct, 100-pct), Offset: offset, Color: bucketColors[i],
			Label: report.BucketLabels[i], Value: v, Pct: int(math.Round(pct)),
		})
		offset -= pct
		if offset < 0 {
			offset += 100
		}
	}
	return segs, total
}

// widthPct is the bar width of a value relative to max, at least 2% when non-zero so it stays visible.
func widthPct(v, maxVal int64) string {
	if maxVal <= 0 || v <= 0 {
		return "0%"
	}
	p := float64(v) / float64(maxVal) * 100
	return fmt.Sprintf("%.1f%%", math.Max(p, 2))
}

// deltaText compares this month to last month, e.g. "↑ %12 (geçen ay)".
func deltaText(cur, prev int64) (text string, up bool, ok bool) {
	if prev <= 0 {
		return "", false, false
	}
	pct := float64(cur-prev) / float64(prev) * 100
	up = pct >= 0
	arrow := "↓"
	if up {
		arrow = "↑"
	}
	return fmt.Sprintf("%s %%%d geçen aya göre", arrow, int(math.Abs(math.Round(pct)))), up, true
}
