<div align="center">

# 🐾 NovelClaw <sub>v1.0.0</sub>

**Single-Binary Web Novel Importer, AI-Assisted Translation Engine & Distraction-Free Reader**

*Written in Go · Zero External Dependencies · Local-First Architecture · Embedded OLED Web Reader*

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev/)
[![Release](https://img.shields.io/badge/Release-v1.0.0-10b981?style=for-the-badge)](https://github.com/Stxyu-p/NovelClaw/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-f59e0b?style=for-the-badge)](LICENSE)
[![Language: TH](https://img.shields.io/badge/Language-%E0%B8%A0%E0%B8%B2%E0%B8%A9%E0%B8%B2%E0%B9%84%E0%B8%97%E0%B8%A2-blue?style=for-the-badge)](#-ภาษาไทย-thai-overview)

![Architecture](https://img.shields.io/badge/Architecture-Single%20Binary-blueviolet?style=flat-square)
![Dependencies](https://img.shields.io/badge/Runtime%20Deps-Zero-success?style=flat-square)
![Storage](https://img.shields.io/badge/Storage-Local--First%20JSON%20%26%20FS-blue?style=flat-square)
![Network](https://img.shields.io/badge/LAN%20Sharing-Responsive%20Web-orange?style=flat-square)

</div>

---

## ⚡ Overview

**NovelClaw** is a local-first, high-throughput web novel management platform engineered in **Go (Golang)**. It packages a full web server, multi-source web scraper, 9Router/LLM translation pipeline with persistent glossary injection, and a distraction-free OLED reader into a **single, standalone executable** (`novelclaw.exe`).

No Node.js runtime, Python virtual environment, Docker daemon, or external SQL database required.

---

## 🧭 Architectural Dataflow

```mermaid
flowchart LR
    A[Web Novel / Raw Text] -->|Scraper Engine| B(NovelClaw Core)
    B --> C{Translation Pipeline}
    
    subgraph C [9Router / LLM Engine]
        C1[Glossary & Character Names]
        C2[Rolling Context Memory]
        C3[Paragraph QA & Auto-Retry]
    end

    C -->|Server-Sent Events| D[Embedded Web Reader]
    C -->|Export Engine| E[EPUB / Markdown / TXT]
    
    D & E --> F[(Local-First Filesystem Store)]
```

---

## 🚀 Key Highlights & Engineering Advantages

| Highlight | Description | Technical Advantage |
| :--- | :--- | :--- |
| 📦 **Single-Binary Delivery** | Web server, scraper, LLM client, and frontend assets embedded directly in the binary. | Zero runtime setup; double-click `novelclaw.exe` to run anywhere. |
| 🛡️ **100% Local-First Storage** | All novel chapters, metadata, reading bookmarks, and glossaries live in simple local files. | Instant offline reading; zero telemetry, zero vendor lock-in. |
| 🧠 **AI Consistency Engine** | Custom prompt compiler integrating character glossaries, martial art techniques, and rolling chapter context. | Produces coherent, natural Thai prose without pronoun drift or lost context. |
| ⚡ **Live SSE Streaming** | Translation jobs stream status and paragraph diffs in real-time over Server-Sent Events (`/api/events`). | Zero client-side polling timer; instant reconnection recovery. |
| 📖 **Distraction-Free Reader** | Built-in web reader featuring OLED Dark, Sepia, and Light modes with font size and layout controls. | Fully responsive across desktop browsers and mobile devices on local LAN. |
| 📚 **Universal Export** | Export any novel or chapter range into structured EPUB, Markdown, or raw TXT. | Streaming temporary-file generation prevents memory bloat on large 1,000+ chapter books. |

---

## 📊 System Footprint & Benchmarks

| Metric | Measured Value | Comparison / Note |
| :--- | :--- | :--- |
| **Binary Size** | ~15 MB | Everything included (server + HTML/CSS/JS assets) |
| **Startup Latency** | < 45 ms | Instant socket binding on port 4890 |
| **Idle Memory (RSS)** | ~24 MB | Minimal GC footprint |
| **External Dependencies** | **0** | No Node.js, Python, or SQLite required |
| **LAN Capability** | 100% Native | Accessible by phones/tablets via `http://[LAN-IP]:4890` |

---

## 🛠️ Quick Start

### 1. Run the Single Binary
Download the pre-built executable and run:

```powershell
.\novelclaw.exe
```

Or specify custom options via CLI flags:

```powershell
.\novelclaw.exe -port 4890 -router "http://localhost:20128/v1" -model "google/gemini-2.5-flash"
```

### 2. Access the Reader
Open your web browser at:
- **Local Machine:** `http://localhost:4890`
- **Mobile on Same WiFi / LAN:** `http://[YOUR-PC-LOCAL-IP]:4890`

---

## ⚙️ CLI Flags & Configuration

| Flag | Description | Default |
| :--- | :--- | :--- |
| `-port` | HTTP port for the web server and reader | `4890` |
| `-data` | Directory path for local novel storage | `./novels` |
| `-router` | Base URL of 9Router or OpenAI-compatible endpoint | `http://localhost:20128/v1` |
| `-model` | LLM model identifier used for translation | `google/gemini-2.5-flash` |
| `-key` | API authorization key (if required) | `""` |

---

## 📁 Project Structure

```text
NovelClaw/
├── main.go                     # Application entry point & browser auto-launcher
├── novelclaw.exe               # Standalone production binary
├── novels/                     # Local filesystem database (JSON & Markdown)
├── internal/
│   ├── config/                 # System configuration & flag parsing
│   ├── model/                  # Domain types and schema contracts
│   ├── storage/                # Safe concurrent JSON & chapter storage engine
│   ├── scraper/                # Web novel parser & sanitization engine
│   ├── translator/             # LLM client, glossary parser & context prompt
│   ├── api/                    # REST endpoints & Server-Sent Events (SSE)
│   └── web/                    # Embedded HTML, OLED CSS & Reader JavaScript
└── tests/                      # Automated browser smoke & API test suites
```

---

## 🧪 Verification & Automated Tests

All tests run locally to certify data integrity, streaming performance, and UI responsiveness:

```powershell
# 1. Run all Go package tests
go test ./...

# 2. Race condition checks for storage and API engines
go test -race ./internal/storage ./internal/api

# 3. Micro-benchmarks for reader decode and export streaming
go test ./internal/storage -run '^$' -bench BenchmarkReaderDecode -benchmem -count 3
go test ./internal/api -run '^$' -bench BenchmarkExport -benchmem -count 3

# 4. Browser smoke tests (Playwright)
node tests/browser-smoke.cjs
```

---

## 🇹🇭 ภาษาไทย (Thai Overview)

NovelClaw คือระบบ **Local-first** สำหรับนำเข้านิยาย แปลด้วย AI คุณภาพสูง และอ่านผ่านเว็บเบราว์เซอร์ในเครื่องหรือมือถือผ่านวง LAN เดียวกัน
- รวมทุกอย่างไว้ในไฟล์เดียว (`novelclaw.exe`) ไม่ต้องติดตั้ง Node.js หรือ Database
- มีระบบ **Glossary** บันทึกชื่อตัวละคร/วิชา/สถานที่ และ **Context Memory** ช่วยให้สำนวนไทยสละสลวยคงเส้นคงวา
- โหมดอ่านหนังสือแบบ OLED Dark, Sepia, และ Light พร้อมระบบบันทึกตอนที่อ่านค้างไว้อัตโนมัติ

---

## 📄 License

Distributed under the [MIT License](LICENSE).  
Copyright (c) 2026 P Choke & SORA.

---

## 🌟 Featured Engineering Projects

A curated collection of local-first, zero-telemetry, and performance-critical systems built by [@Stxyu-p](https://github.com/Stxyu-p):

| Project | Platform / Target | Architecture & Core Highlights | Links & Distribution |
| :--- | :--- | :--- | :--- |
| **🐾 [NovelClaw](https://github.com/Stxyu-p/NovelClaw)** | Web Novels / AI Reader | Single-binary Go application (`novelclaw.exe`) with embedded zero-dependency web reader, 9Router/LLM translation pipeline, persistent glossary & context memory, and SSE job streaming. | [![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://github.com/Stxyu-p/NovelClaw) · [GitHub](https://github.com/Stxyu-p/NovelClaw) |
| **⚡ [IG MaxPland](https://github.com/Stxyu-p/ig-maxpland)** | Instagram Web | Clean Architecture (18 decoupled modules), stealth seen-telemetry interceptor (`fetch`/`XHR`/`sendBeacon`), zero-bounce clean feed engine, and safe dormant radar with randomized jitter pacing. | [![Greasy Fork](https://img.shields.io/badge/Greasy%20Fork-v3.0.0-red?style=flat-square&logo=greasyfork&logoColor=white)](https://greasyfork.org/th/scripts/595787-ig-maxpland) · [GitHub](https://github.com/Stxyu-p/ig-maxpland) |
| **⚡ [Telefilter Desktop](https://github.com/Stxyu-p/telefilter-desktop)** | Telegram WebK | Ultra-compact 34px inline toolbar, pure client-side ZIP32 multi-album packing engine, deep virtualized DOM harvester, and 100% client-side privacy vault. | [![Greasy Fork](https://img.shields.io/badge/Greasy%20Fork-v5.0.0-red?style=flat-square&logo=greasyfork&logoColor=white)](https://greasyfork.org/th/scripts/596222-telefilter-desktop-edition-v5) · [GitHub](https://github.com/Stxyu-p/telefilter-desktop) |
| **⚡ [ThreadMax](https://github.com/Stxyu-p/threadmax)** | Threads Web | 1-Click carousel & bulk media extraction with in-memory ZIP32 compiler, video speed booster & PiP, clean link tracking sanitizer, and clean reader thread unroller. | [![Tampermonkey](https://img.shields.io/badge/Tampermonkey-Userscript-00485B?style=flat-square&logo=tampermonkey&logoColor=white)](https://github.com/Stxyu-p/threadmax) · [GitHub](https://github.com/Stxyu-p/threadmax) |
| **🧠 [memcore](https://github.com/Stxyu-p/memcore)** | Multi-Agent Memory | Governed, local-first memory engine for multi-agent workflows — SQLite + WAL + FTS5, immutable versioning, tombstone guards, and journal-first admission. | [![GitHub](https://img.shields.io/badge/Release-v0.6.0-10b981?style=flat-square&logo=github&logoColor=white)](https://github.com/Stxyu-p/memcore) · [GitHub](https://github.com/Stxyu-p/memcore) |

<div align="center">
<sub>Crafted with engineering discipline · Local-First · Zero Telemetry · High Performance</sub>
</div>
