# BizOps — One-File Revenue & Risk Intelligence (Go)

BizOps is a single-file Go application that ingests sales/invoice data and turns it into actionable business intelligence. Upload a CSV (or run in CLI mode) to get:

* Core KPIs: revenue, orders, AOV, unique customers, retention

* Top customers/products

* Daily revenue with anomaly detection

* Overdue/unpaid detection

* A simple 7-day naїve forecast

* Recommendations that translate insights into next actions

* Optional Slack alerts and optional AI executive summary


# ✨ Feature Highlights

* CSV ingest (flexible headers, forgiving date parsing)

* KPIs: Revenue, Orders, AOV, Unique Customers, Retention (week-over-week repeat rate)

* Daily Revenue Chart (inline SVG — no JS required)

* Anomaly Detection 

* Forecast (7-day average projected over next week)

* Credit Risk (flags “overdue”/“unpaid” rows)

* Recommendations (clear, prioritized next steps)

* Two modes:

    * CLI → generates report.md

    * Web server → HTML dashboard + JSON API

* Optional Integrations

    * Slack alerts via SLACK_WEBHOOK

    * AI summary via OPENAI_API_KEY (uses OpenAI Chat Completions API)

# 🧩 Data Format (CSV)

* Headers are matched case-insensitively and flexibly. Recommended columns:

Column	Type	Notes
date	Date	Accepts YYYY-MM-DD, YYYY/MM/DD, RFC3339, etc.
customer	String	Customer identifier or name
product	String	SKU / product name
amount	Number	Positive revenue
status	String	Free text; flags if contains overdue, unpaid, due

* Sample (sample.csv):

date,customer,product,amount,status
2025-07-01,Acme Corp,Widget A,199.00,paid
2025-07-01,Acme Corp,Widget B,89.00,paid
2025-07-02,Zen LLC,Widget A,199.00,unpaid
2025-07-03,Atlas Inc,Widget C,349.00,paid
2025-07-04,Zen LLC,Widget A,199.00,overdue
2025-07-05,Acme Corp,Widget A,199.00,paid

# 🚀 How to Run
# Prereqs

Go 1.20+

(Optional) Slack incoming webhook URL

(Optional) OpenAI API key (for an AI exec summary)

1) CLI Mode (generates report.md)

Windows (PowerShell):

go run main.go -file="sample.csv"


macOS/Linux:

go run main.go -file=sample.csv


Outputs a Markdown report: report.md

2) Web Server Mode (HTML Dashboard + JSON API)

Windows (PowerShell):

go run main.go -serve -port=8080
# open http://localhost:8080


macOS/Linux:

go run main.go -serve -port=8080


Upload your CSV via the form.

See KPIs, chart, anomalies, and recommendations.

Hit JSON at GET /api/kpis.

#🔌 Optional Integrations

Slack Alerts (fires when anomalies or overdue items detected)

# PowerShell
$env:SLACK_WEBHOOK = "https://hooks.slack.com/services/..."
go run main.go -file="sample.csv"

# macOS/Linux
export SLACK_WEBHOOK="https://hooks.slack.com/services/..."
go run main.go -file=sample.csv


AI Executive Summary (concise 3–4 sentence exec readout)

# PowerShell
$env:OPENAI_API_KEY = "sk-..."
go run main.go -serve -port=8080

# macOS/Linux
export OPENAI_API_KEY="sk-..."
go run main.go -serve -port=8080


If not set, the app simply skips the feature—no errors.

# 🌐 HTTP Endpoints

* GET / — HTML dashboard; upload form & visualizations

* POST /upload — multipart CSV upload; computes & caches KPIs; redirects to /

* GET /api/kpis — returns latest KPIs as JSON:

{
  "totalRevenue": 1135.0,
  "avgOrderValue": 189.17,
  "orders": 6,
  "uniqueCustomers": 3,
  "retentionRate": 0.33,
  "forecastNext7DaysTotal": 945.0,
  "topCustomers": [{"key":"Acme Corp","value":587.0}],
  "topProducts": [{"key":"Widget A","value":597.0}],
  "dailyRevenue": [{"day":"2025-07-01T00:00:00Z","value":288.0}],
  "anomalies": [{"day":"2025-07-04T00:00:00Z","value":199.0,"z":2.10}],
  "overdueCount": 2,
  "overdueTotal": 398.0,
  "suggestions": ["Initiate dunning workflow..."],
  "from":"2025-07-01T00:00:00Z",
  "to":"2025-07-07T00:00:00Z",
  "execSummary": "..."
}

# 📈 What You Can Expect

* Fast insight from messy CSVs: forgiving parsing + flexible header matching.

* Business-ready KPIs: leadership can see the story in seconds.

* Early warnings: anomaly detection flags dips/spikes for root-cause analysis.

* Cash risk visibility: overdue/unpaid totals & counts for collections prioritization.

* Next-step suggestions: concrete actions (dunning, bundling, loyalty offers, etc.).

* Lightweight forecast: naïve 7-day projection to inform staffing/inventory.

* Exec summary (optional): compact narrative for daily standups or investor updates.

# 🧠 Why It’s Useful (Real Scenarios)

* Founder/GM Daily Pulse: Drop in yesterday’s CSV → get trend, risks, and “what to do.”

* RevOps / Finance: Monitor overdue growth; trigger collections when a threshold hits.

* Marketing: Detect spikes tied to campaigns; double down where causality is likely.

* Sales: Identify top customers; design tailored upsell/retention plays.

* Support: Watch dips possibly tied to incidents; coordinate with engineering.

# 🛠️ Architecture at a Glance

* Single Go binary (one file for portability)

* CSV → typed records → in-memory aggregates

* KPI computation: maps + slices, sorted views

* Anomaly calc: simple z-score over daily revenue

* Forecast: last-N moving average × 7

* HTML rendered via Go templates (inline SVG)

* Optional HTTP calls to Slack/OpenAI

* Ephemeral state (resets on restart); perfect for demos & local runs

# 🔒 Security & Privacy

* No .env required by default. If you use integrations, never commit real keys.

* Add a .gitignore:

* Use synthetic demo data when sharing publicly.

* If you ever accidentally commit secrets, rotate them immediately.

# 🧪 Troubleshooting

* “no KPIs yet” on /api/kpis
   Upload a CSV first (web mode) or run CLI with -file.

* Dates not parsing
   Use YYYY-MM-DD or RFC3339. Other formats are attempted but not guaranteed.

* No anomalies detected
   Not an error; either your data is stable or the z-score threshold wasn’t crossed.

* Slack messages not arriving
   Check SLACK_WEBHOOK validity and firewall/proxy settings.

* AI summary empty
   OPENAI_API_KEY not set, or the API call failed—app continues without it.
