# 🐾 NovelClaw — Single Binary Novel Importer, Translator & Reader

NovelClaw คือระบบ **Local-first** สำหรับนำเข้านิยาย แปลด้วย AI คุณภาพสูง และอ่านผ่านเว็บเบราว์เซอร์ในเครื่องหรือมือถือผ่านวง LAN เดียวกัน
เขียนด้วยภาษา **Go (Golang)** รวมทุกอย่างไว้ในไฟล์เดียว (`novelclaw.exe`) โดยไม่ต้องพึ่งพา Node.js, Python venv, หรือ Database ภายนอก

---

## ⚡ จุดเด่น
- **Single Binary:** ไฟล์ `.exe` เดียวมีทั้ง Web Server, Web Scraper, AI Translation Engine, และ Reader UI ฝังตัวในไฟล์ ขนาดขึ้นอยู่กับ Go version และ build flags
- **Local-first:** อ่านไฟล์ในเครื่องได้โดยไม่ต้องเชื่อมต่อ AI; ใช้สารบัญแบบแบ่งหน้าและจำกัด cache เสียงอ่าน ไม่ต้องติดตั้ง database หรือ JavaScript runtime เพื่อใช้งาน
- **Universal Import:** ดึงนิยายจากเว็บ (เช่น 69shu และเว็บทั่วไป) พร้อมระบบตัดโฆษณา/ตัวอักษรขยะ หรือวางข้อความดิบ
- **High-Quality AI Translation:** เชื่อมต่อกับ 9Router / OpenRouter พร้อมระบบ **Glossary** (ชื่อตัวละคร/วิชา/สถานที่) และ **Context Memory** จากตอนก่อนหน้าเพื่อสำนวนไทยที่สละสลวย
- **Distraction-Free Web Reader:** รองรับธีม Dark (OLED), Sepia, Light, ปรับฟอนต์/ขนาด, บันทึกตอนที่อ่านค้างไว้อัตโนมัติ

---

## 🚀 การเริ่มใช้งาน (Quick Start)

รัน Single Binary โดยตรง:
```powershell
.\novelclaw.exe
```

หากต้องการกำหนดพอร์ตเอง:
```powershell
.\novelclaw.exe -port 4890
```

เปิดเบราว์เซอร์ที่:
- **บนคอมพิวเตอร์:** `http://localhost:4890`
- **บนมือถือ (ในวง LAN เดียวกัน):** `http://[IP-เครื่อง-PC]:4890`

---

## 🛠️ คำสั่งปรับแต่ง (CLI Flags)
```powershell
.\novelclaw.exe -port 4890 -router "http://localhost:20128/v1" -model "google/gemini-2.5-flash"
```

| Flag | คำอธิบาย | ค่าเริ่มต้น |
| :--- | :--- | :--- |
| `-port` | Port สำหรับเปิด Web Server | `4890` |
| `-data` | โฟลเดอร์เก็บข้อมูลนิยาย | `./novels` |
| `-router` | Base URL ของ 9Router หรือ OpenAI endpoint | `http://localhost:20128/v1` |
| `-model` | ชื่อ AI Model ที่ต้องการใช้แปล | `google/gemini-2.5-flash` |
| `-key` | API Key (ถ้ามี) | `""` |

---

## 📁 โครงสร้างโปรเจกต์
```
NovelClaw/
├── main.go                     # Entry point (+ launcher.go เปิดเบราว์เซอร์อัตโนมัติ)
├── novelclaw.exe               # Single Binary สำเร็จรูป (build ด้วย: go build -o novelclaw.exe .)
├── novels/                     # โฟลเดอร์เก็บข้อมูลนิยาย (JSON/Markdown)
├── scripts/
│   ├── batch_titles.go         # Utility แปลชื่อตอนย้อนหลัง (รัน: go run scripts/batch_titles.go)
│   └── qa-archived/            # สคริปต์ QA แบบ one-off (เก็บไว้อ้างอิง ไม่ใช้แล้ว)
├── internal/
│   ├── config/                 # การตั้งค่าระบบ
│   ├── model/                  # Data structures
│   ├── storage/                # JSON & Filesystem Store
│   ├── scraper/                # ตัวดึงเนื้อหานิยายจากเว็บ
│   ├── translator/             # 9Router LLM Client, Prompt & Glossary
│   ├── api/                    # REST API & Server-Sent Events (SSE)
│   └── web/                    # Embedded HTML / CSS / JS Reader
```

## ตรวจสอบก่อนใช้งาน build ใหม่

คำสั่งสำหรับผู้พัฒนา (Node.js 22+ ใช้เฉพาะการทดสอบ ไม่ใช่ dependency ของแอป):

```powershell
go test ./...
go vet ./...
go test -race ./internal/storage ./internal/api
node --test tests/*.test.mjs
go test ./internal/storage -run '^$' -bench BenchmarkReaderDecode -benchmem -count 3
go test ./internal/api -run '^$' -bench BenchmarkExport -benchmem -count 3
go build -o novelclaw.exe .
```

Browser smoke ใช้ Playwright ที่ติดตั้งในเครื่อง:

```powershell
node tests/browser-smoke.cjs
```

ตั้ง `NC_BINARY` หาก executable อยู่ที่อื่น และ `NODE_PATH` หากใช้ Playwright จากโฟลเดอร์ runtime ภายนอก ชุดทดสอบสร้างข้อมูลจำลอง 10,000 ตอนใน temporary directory และล้างหลังจบ ไม่อ่านหรือเปลี่ยนนิยายจริง ครอบคลุมการอ่านต่อ เปลี่ยนตอน ขนาดตัวอักษร keyboard shortcuts และหน้าจอ 320/390 px

การอ่าน Local ไม่เรียกค้นหาโมเดลออนไลน์ตอนเปิดแอป; ค้นหารายชื่อโมเดลได้จากหน้าตั้งค่า การแปลและเสียงอ่าน Neural ยังต้องใช้ provider/gateway ที่ตั้งค่าไว้ ปริมาณ RAM และความเร็วจริงขึ้นอยู่กับความยาวบท เบราว์เซอร์ ขนาดคลัง และงานที่กำลังทำงาน

Browser smoke ยังตรวจการนำเข้าข้อความ ส่งออก TXT/Markdown/EPUB สำรองข้อมูล และเปิด glossary, memory/QA, settings โดยจำลองรายชื่อโมเดล การทดสอบ Go ใช้ provider จำลองเพื่อตรวจเส้นทางนำเข้า → แปล → อ่าน → ส่งออก รวมถึงการเก็บคำแปลเดิมเมื่อโมเดลส่งย่อหน้าไม่ครบ

งานที่ยังทำอยู่กู้สถานะผ่าน `GET /api/jobs` เมื่อ SSE เชื่อมต่อใหม่ โดยไม่มี polling timer การส่งออกใช้ temporary file และประมวลผลทีละบทเพื่อลด allocation ของหนังสือยาว ต้องมีพื้นที่ว่างสำหรับไฟล์ส่งออก การสำรองข้อมูลจะเก็บและหมุนเวียนเฉพาะไฟล์ที่ตรงชื่อ backup ของ NovelClaw

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

