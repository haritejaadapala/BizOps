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
