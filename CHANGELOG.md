# Changelog

All notable changes to NovelClaw are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-10-06

### Added
- **Single-Binary Standalone Core**: Complete web server, crawler engine, and reader UI bundled into a single standalone Go binary (`novelclaw.exe`) with zero external runtime dependencies.
- **AI Translation & Glossary Engine**: Integration with 9Router local/remote gateway featuring persistent character glossary injection and story memory tracking.
- **Multi-Source Novel Scraper**: Resilient chapter extraction pipeline supporting major web novel portals and raw text files.
- **Embedded OLED Web Reader**: Responsive, distraction-free reading interface with typography customization, reading position sync, and local LAN sharing.
- **Streaming Export**: High-speed, streaming EPUB and plain-text export pipeline with zero disk buffering.

### Security
- Hardened SSRF boundary preventing requests to localhost, loopback, private RFC-1918 subnets, and DNS-rebinding vectors.
