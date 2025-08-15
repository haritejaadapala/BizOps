// BizPulse - One-file Revenue & Risk Intelligence Server
// Features: CSV ingest, KPIs, anomalies, overdue detection, forecast, suggestions,
// Slack alerts, optional OpenAI exec summary, HTML dashboard + JSON API, CLI report.
//
// Run:
//   go run main.go -file=data.csv        # CLI mode -> report.md
//   go run main.go -serve -port=8080     # Web mode -> upload & dashboard
//
// CSV expected headers (case-insensitive): date, customer, product, amount, status
// - date: YYYY-MM-DD (flexible parsing attempted)
// - amount: float (positive for revenue)
// - status: "paid" / "unpaid" / "overdue" (free text ok; we'll detect unpaid/overdue heuristically)

package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// -------- Data model --------

type Sale struct {
	Date    time.Time
	Customer string
	Product  string
	Amount   float64
	Status   string
}

type KPIs struct {
	From, To               time.Time
	TotalRevenue           float64
	AvgOrderValue          float64
	Orders                 int
	UniqueCustomers        int
	TopCustomers           []KVf
	TopProducts            []KVf
	DailyRevenue           []KVt
	RetentionRate          float64
	ForecastNext7DaysTotal float64
	Anomalies              []Anomaly
	OverdueCount           int
	OverdueTotal           float64
	Suggestions            []string
	ExecSummary            string // optional (OpenAI)
}

type KVf struct {
	Key   string
	Value float64
}

type KVt struct {
	Day   time.Time
	Value float64
}

type Anomaly struct {
	Day   time.Time
	Value float64
	Z     float64
}

// -------- CSV ingest --------

func parseCSV(r io.Reader) ([]Sale, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv read: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("csv has no data rows")
	}
	// Header map
	h := map[string]int{}
	for i, col := range records[0] {
		h[strings.ToLower(strings.TrimSpace(col))] = i
	}
	get := func(row []string, key string) string {
		for k, idx := range h {
			if strings.Contains(k, key) { // flexible match
				if idx >= 0 && idx < len(row) {
					return strings.TrimSpace(row[idx])
				}
			}
		}
		return ""
	}
	var out []Sale
	for _, row := range records[1:] {
		ds := get(row, "date")
		if ds == "" { continue }
		dt := parseDateFlexible(ds)
		if dt.IsZero() { continue }
		amtStr := get(row, "amount")
		amt, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(amtStr), ",", ""), 64)
		s := Sale{
			Date:     dt,
			Customer: nz(get(row, "customer"), "Unknown"),
			Product:  nz(get(row, "product"), "Unknown"),
			Amount:   amt,
			Status:   strings.ToLower(get(row, "status")),
		}
		out = append(out, s)
	}
	return out, nil
}

func parseDateFlexible(s string) time.Time {
	candidates := []string{
		"2006-01-02", "02/01/2006", "01/02/2006", "2006/01/02", "2006.01.02", time.RFC3339,
	}
	s = strings.TrimSpace(s)
	for _, f := range candidates {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	// Try partial
	if t, err := time.Parse("2006-01", s); err == nil { return t }
	return time.Time{}
}

func nz(a, b string) string {
	if strings.TrimSpace(a) == "" { return b }
	return a
}

// -------- Analytics --------

func computeKPIs(sales []Sale) KPIs {
	if len(sales) == 0 { return KPIs{} }
	sort.Slice(sales, func(i,j int) bool { return sales[i].Date.Before(sales[j].Date) })
	from, to := sales[0].Date, sales[len(sales)-1].Date

	var total float64
	orders := 0
	byCustomer := map[string]float64{}
	byProduct  := map[string]float64{}
	customers  := map[string]bool{}
	// daily
	dr := map[string]float64{}
	// overdue
	overdueCount := 0
	var overdueTotal float64

	for _, s := range sales {
		total += s.Amount
		orders++
		byCustomer[s.Customer] += s.Amount
		byProduct[s.Product] += s.Amount
		customers[s.Customer] = true
		key := s.Date.Format("2006-01-02")
		dr[key] += s.Amount
		// detect overdue/unpaid heuristics
		if strings.Contains(s.Status, "overdue") || strings.Contains(s.Status, "unpaid") || strings.Contains(s.Status, "due") {
			overdueCount++
			overdueTotal += s.Amount
		}
	}

	// to slice
	var daily []KVt
	for k,v := range dr {
		d, _ := time.Parse("2006-01-02", k)
		daily = append(daily, KVt{Day: d, Value: v})
	}
	sort.Slice(daily, func(i,j int) bool { return daily[i].Day.Before(daily[j].Day) })

	// top N
	topCust := topN(byCustomer, 5)
	topProd := topN(byProduct, 5)

	avgOrder := 0.0
	if orders > 0 {
		avgOrder = total / float64(orders)
	}

	// retention (very rough): % of customers appearing in >=2 distinct weeks
	retention := retentionRate(sales)

	// anomalies on daily revenue
	anoms := detectAnomalies(daily)

	// forecast 7-day naive (moving average over last 7 or up to 14 days)
	forecast := forecast7(daily)

	// suggestions
	sug := suggestions(total, avgOrder, overdueCount, overdueTotal, topCust, topProd, anoms)

	return KPIs{
		From: from, To: to,
		TotalRevenue: total,
		AvgOrderValue: avgOrder,
		Orders: orders,
		UniqueCustomers: len(customers),
		TopCustomers: topCust,
		TopProducts: topProd,
		DailyRevenue: daily,
		RetentionRate: retention,
		ForecastNext7DaysTotal: forecast,
		Anomalies: anoms,
		OverdueCount: overdueCount,
		OverdueTotal: overdueTotal,
		Suggestions: sug,
	}
}

func topN(m map[string]float64, n int) []KVf {
	var arr []KVf
	for k,v := range m { arr = append(arr, KVf{k,v}) }
	sort.Slice(arr, func(i,j int) bool { return arr[i].Value > arr[j].Value })
	if len(arr) > n { arr = arr[:n] }
	return arr
}

func retentionRate(sales []Sale) float64 {
	// crude: map customer -> set of ISO weeks
	type wk struct{ year, week int }
	m := map[string]map[wk]bool{}
	for _, s := range sales {
		year, week := s.Date.ISOWeek()
		w := wk{year,week}
		if _, ok := m[s.Customer]; !ok { m[s.Customer] = map[wk]bool{} }
		m[s.Customer][w] = true
	}
	retained := 0
	for _, set := range m {
		if len(set) >= 2 { retained++ }
	}
	if len(m) == 0 { return 0 }
	return float64(retained) / float64(len(m))
}

func detectAnomalies(d []KVt) []Anomaly {
	if len(d) < 7 { return nil }
	// compute mean & std
	var sum float64
	for _, x := range d { sum += x.Value }
	mean := sum / float64(len(d))
	var ss float64
	for _, x := range d { ss += (x.Value - mean) * (x.Value - mean) }
	std := math.Sqrt(ss / float64(len(d)))
	if std == 0 { return nil }
	var out []Anomaly
	for _, x := range d {
		z := (x.Value - mean) / std
		if math.Abs(z) >= 2.0 { // flag 2+ std
			out = append(out, Anomaly{Day: x.Day, Value: x.Value, Z: z})
		}
	}
	return out
}

func forecast7(d []KVt) float64 {
	if len(d) == 0 { return 0 }
	window := 7
	if len(d) < window { window = len(d) }
	var sum float64
	for i:=len(d)-window; i<len(d); i++ {
		sum += d[i].Value
	}
	avg := sum / float64(window)
	return avg * 7.0
}

func suggestions(total, aov float64, overdueCount int, overdueTotal float64, topC, topP []KVf, anoms []Anomaly) []string {
	var s []string
	if overdueCount > 0 {
		s = append(s, fmt.Sprintf("Initiate dunning workflow: %d overdue/unpaid invoices totaling $%.2f.", overdueCount, overdueTotal))
	}
	if aov < 50 {
		s = append(s, "Test bundles/tiers to increase Average Order Value (cross-sell top products).")
	}
	if len(topC) > 0 {
		s = append(s, fmt.Sprintf("Send loyalty offers to top customers: %s.", joinKV(topC)))
	}
	if len(topP) > 0 {
		s = append(s, fmt.Sprintf("Double down on high-velocity products: %s.", joinKV(topP)))
	}
	for _, an := range anoms {
		if an.Z < -2 {
			s = append(s, fmt.Sprintf("Investigate revenue dip on %s (z=%.2f). Check campaigns, outages, pricing.", an.Day.Format("2006-01-02"), an.Z))
		} else if an.Z > 2 {
			s = append(s, fmt.Sprintf("Spike on %s (z=%.2f). Attribute uplift and try to replicate.", an.Day.Format("2006-01-02"), an.Z))
		}
	}
	if total > 0 && aov > 0 && overdueCount == 0 && len(anoms) == 0 {
		s = append(s, "Steady performance. Consider experimentation (price tests, reorder nudges) to uncover upside.")
	}
	return s
}

func joinKV(a []KVf) string {
	var parts []string
	for _, x := range a {
		parts = append(parts, fmt.Sprintf("%s ($%.2f)", x.Key, x.Value))
	}
	return strings.Join(parts, ", ")
}

// -------- Slack + OpenAI (optional) --------

func postSlack(webhook string, msg string) {
	if webhook == "" { return }
	body := map[string]string{"text": msg}
	b, _ := json.Marshal(body)
	http.Post(webhook, "application/json", bytes.NewReader(b))
}

func openAISummary(ctx context.Context, k KPIs) string {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" { return "" }
	// minimal raw HTTP call to OpenAI Chat Completions (gpt-4o-mini)
	payload := fmt.Sprintf(`{"model":"gpt-4o-mini","messages":[{"role":"system","content":"You write concise executive summaries for business performance."},{"role":"user","content":"Summarize these KPIs in 4 sentences, include 1-2 risks and 1-2 actionable next steps.\nFrom:%s To:%s\nRevenue: %.2f\nOrders: %d\nAOV: %.2f\nRetention: %.2f\nTopCustomers: %s\nTopProducts: %s\nOverdue: %d ($%.2f)\nForecast7: %.2f"}],"temperature":0.2}`,
		k.From.Format("2006-01-02"), k.To.Format("2006-01-02"),
		k.TotalRevenue, k.Orders, k.AvgOrderValue, k.RetentionRate,
		joinKV(k.TopCustomers), joinKV(k.TopProducts), k.OverdueCount, k.OverdueTotal, k.ForecastNext7DaysTotal,
	)
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil { return "" }
	defer resp.Body.Close()
	var raw struct{
		Choices []struct{ Message struct{ Content string `json:"content"` } `json:"message"` } `json:"choices"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&raw)
	if len(raw.Choices) > 0 {
		return strings.TrimSpace(raw.Choices[0].Message.Content)
	}
	return ""
}

// -------- HTML + API + CLI --------

var tpl = template.Must(template.New("page").Parse(`
<!doctype html><html><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>BizPulse</title>
<style>
body{font-family:system-ui,Segoe UI,Roboto,Inter,Arial;background:#0b1020;color:#e8ecff;margin:0;padding:20px}
.card{background:#111837;border:1px solid #203063;border-radius:14px;padding:16px;margin:12px 0}
h1{margin:0 0 10px 0} .muted{color:#9aa7cf} table{width:100%;border-collapse:collapse}
th,td{border-bottom:1px solid #22305f;padding:8px;vertical-align:top}
.badge{display:inline-block;background:#1b2a59;padding:4px 8px;border-radius:8px;margin-right:6px}
svg{max-width:100%}
button{background:#7aa2ff;color:#04102a;border:none;padding:8px 12px;border-radius:10px;cursor:pointer}
input[type=file]{margin-top:8px}
</style>
</head><body>
<h1>BizPulse</h1>
<div class="card">
  <h3>Upload CSV</h3>
  <form method="POST" action="/upload" enctype="multipart/form-data">
    <input type="file" name="file" required>
    <button type="submit">Analyze</button>
  </form>
  <p class="muted">Columns: date, customer, product, amount, status (flexible order)</p>
</div>

{{if .KPIs}}
<div class="card">
  <h3>KPIs ({{.KPIs.From.Format "2006-01-02"}} → {{.KPIs.To.Format "2006-01-02"}})</h3>
  <div class="badge">Revenue: ${{printf "%.2f" .KPIs.TotalRevenue}}</div>
  <div class="badge">Orders: {{.KPIs.Orders}}</div>
  <div class="badge">AOV: ${{printf "%.2f" .KPIs.AvgOrderValue}}</div>
  <div class="badge">Unique Customers: {{.KPIs.UniqueCustomers}}</div>
  <div class="badge">Retention: {{printf "%.1f" (mul100 .KPIs.RetentionRate)}}%</div>
  <div class="badge">Forecast 7d: ${{printf "%.2f" .KPIs.ForecastNext7DaysTotal}}</div>
</div>

<div class="card">
  <h3>Daily Revenue</h3>
  {{ svgSpark .KPIs.DailyRevenue }}
  {{ if .KPIs.Anomalies }}
  <p class="muted">Anomalies: {{len .KPIs.Anomalies}}</p>
  {{end}}
</div>

<div class="card">
  <h3>Top Customers</h3>
  <table><thead><tr><th>Customer</th><th>Revenue</th></tr></thead><tbody>
  {{range .KPIs.TopCustomers}}<tr><td>{{.Key}}</td><td>${{printf "%.2f" .Value}}</td></tr>{{end}}
  </tbody></table>
</div>

<div class="card">
  <h3>Top Products</h3>
  <table><thead><tr><th>Product</th><th>Revenue</th></tr></thead><tbody>
  {{range .KPIs.TopProducts}}<tr><td>{{.Key}}</td><td>${{printf "%.2f" .Value}}</td></tr>{{end}}
  </tbody></table>
</div>

<div class="card">
  <h3>Risks & Actions</h3>
  <ul>{{range .KPIs.Suggestions}}<li>{{.}}</li>{{end}}</ul>
  {{if .KPIs.ExecSummary}}
  <h4>Executive Summary (AI)</h4>
  <p class="muted">{{.KPIs.ExecSummary}}</p>
  {{end}}
</div>
{{end}}

</body></html>
`))

// template funcs
func mul100(f float64) float64 { return f*100 }

func svgSpark(d []KVt) template.HTML {
	if len(d) == 0 { return template.HTML("<p class='muted'>No data.</p>") }
	// normalize
	minV, maxV := d[0].Value, d[0].Value
	for _, x := range d {
		if x.Value < minV { minV = x.Value }
		if x.Value > maxV { maxV = x.Value }
	}
	w, h := 600.0, 120.0
	var pts []string
	for i, x := range d {
		px := float64(i) * (w / float64(max(1, len(d)-1)))
		py := h - scale(x.Value, minV, maxV, 8, h-8)
		pts = append(pts, fmt.Sprintf("%.1f,%.1f", px, py))
	}
	path := "M " + strings.Join(pts, " L ")
	svg := fmt.Sprintf(`<svg viewBox="0 0 %.0f %.0f"><path d="%s" fill="none" stroke="#7aa2ff" stroke-width="2"/><line x1="0" y1="%.0f" x2="%.0f" y2="%.0f" stroke="#22305f"/></svg>`, w, h, path, h-0.5, w, h-0.5)
	return template.HTML(svg)
}

func scale(v, min, max, a, b float64) float64 {
	if max == min { return (a+b)/2 }
	return a + (v-min)*(b-a)/(max-min)
}
func max(a,b int) int { if a>b {return a}; return b }

// server state
var latestKPIs *KPIs

func main() {
	var (
		file  = flag.String("file", "", "CSV file to analyze (CLI mode)")
		serve = flag.Bool("serve", false, "Start HTTP server")
		port  = flag.Int("port", 8080, "HTTP port")
	)
	flag.Parse()

	// register funcs
	tpl = tpl.Funcs(template.FuncMap{
		"svgSpark": svgSpark,
		"mul100": mul100,
	})

	if *serve {
		http.HandleFunc("/", handleIndex)
		http.HandleFunc("/upload", handleUpload)
		http.HandleFunc("/api/kpis", handleKPIs)
		addr := fmt.Sprintf(":%d", *port)
		log.Printf("BizPulse server on %s", addr)
		log.Fatal(http.ListenAndServe(addr, nil))
		return
	}

	if *file != "" {
		if err := runCLI(*file); err != nil {
			log.Fatal(err)
		}
		return
	}

	fmt.Println("Usage:")
	fmt.Println("  go run main.go -file=data.csv           # CLI: outputs report.md")
	fmt.Println("  go run main.go -serve -port=8080        # Web: upload & dashboard")
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	var data struct{ KPIs *KPIs }
	data.KPIs = latestKPIs
	_ = tpl.Execute(w, data)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(50<<20); err != nil {
		http.Error(w, err.Error(), 400); return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", 400); return
	}
	defer f.Close()
	sales, err := parseCSV(f)
	if err != nil {
		http.Error(w, "parse: "+err.Error(), 400); return
	}
	k := computeKPIs(sales)
	// AI exec summary (optional)
	if os.Getenv("OPENAI_API_KEY") != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		k.ExecSummary = openAISummary(ctx, k)
	}
	latestKPIs = &k
	// push alerts if anomalies or overdue
	if len(k.Anomalies) > 0 || k.OverdueCount > 0 {
		msg := fmt.Sprintf("BizPulse Alert: %d anomalies; %d overdue ($%.2f). Period %s→%s. Rev $%.2f.",
			len(k.Anomalies), k.OverdueCount, k.OverdueTotal,
			k.From.Format("2006-01-02"), k.To.Format("2006-01-02"), k.TotalRevenue)
		postSlack(os.Getenv("SLACK_WEBHOOK"), msg)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleKPIs(w http.ResponseWriter, _ *http.Request) {
	if latestKPIs == nil {
		http.Error(w, "no KPIs yet", 404); return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(latestKPIs)
}

func runCLI(path string) error {
	f, err := os.Open(path)
	if err != nil { return err }
	defer f.Close()
	sales, err := parseCSV(f)
	if err != nil { return err }
	k := computeKPIs(sales)
	// AI exec summary
	if os.Getenv("OPENAI_API_KEY") != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		k.ExecSummary = openAISummary(ctx, k)
	}
	md := renderMarkdown(k)
	if err := os.WriteFile("report.md", []byte(md), 0644); err != nil {
		return err
	}
	fmt.Println("Wrote report.md")
	// Slack alert if needed
	if len(k.Anomalies) > 0 || k.OverdueCount > 0 {
		msg := fmt.Sprintf("BizPulse Alert: %d anomalies; %d overdue ($%.2f). Period %s→%s. Rev $%.2f.",
			len(k.Anomalies), k.OverdueCount, k.OverdueTotal,
			k.From.Format("2006-01-02"), k.To.Format("2006-01-02"), k.TotalRevenue)
		postSlack(os.Getenv("SLACK_WEBHOOK"), msg)
	}
	return nil
}

func renderMarkdown(k KPIs) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# BizPulse Report (%s → %s)\n\n", k.From.Format("2006-01-02"), k.To.Format("2006-01-02"))
	fmt.Fprintf(&b, "- **Revenue:** $%.2f\n- **Orders:** %d\n- **AOV:** $%.2f\n- **Unique Customers:** %d\n- **Retention:** %.1f%%\n- **Forecast (7d):** $%.2f\n\n",
		k.TotalRevenue, k.Orders, k.AvgOrderValue, k.UniqueCustomers, k.RetentionRate*100, k.ForecastNext7DaysTotal)
	if len(k.TopCustomers) > 0 {
		fmt.Fprintf(&b, "## Top Customers\n")
		for _, kv := range k.TopCustomers {
			fmt.Fprintf(&b, "- %s: $%.2f\n", kv.Key, kv.Value)
		}
		fmt.Fprintln(&b)
	}
	if len(k.TopProducts) > 0 {
		fmt.Fprintf(&b, "## Top Products\n")
		for _, kv := range k.TopProducts {
			fmt.Fprintf(&b, "- %s: $%.2f\n", kv.Key, kv.Value)
		}
		fmt.Fprintln(&b)
	}
	if len(k.Anomalies) > 0 {
		fmt.Fprintf(&b, "## Anomalies\n")
		for _, a := range k.Anomalies {
			fmt.Fprintf(&b, "- %s: $%.2f (z=%.2f)\n", a.Day.Format("2006-01-02"), a.Value, a.Z)
		}
		fmt.Fprintln(&b)
	}
	if k.OverdueCount > 0 {
		fmt.Fprintf(&b, "## Overdue / Unpaid\n- Count: %d\n- Total: $%.2f\n\n", k.OverdueCount, k.OverdueTotal)
	}
	if len(k.Suggestions) > 0 {
		fmt.Fprintf(&b, "## Recommendations\n")
		for _, s := range k.Suggestions {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		fmt.Fprintln(&b)
	}
	if k.ExecSummary != "" {
		fmt.Fprintf(&b, "## Executive Summary (AI)\n%s\n", k.ExecSummary)
	}
	return b.String()
}
