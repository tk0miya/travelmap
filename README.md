# travelmap

GPS 軌跡・チェックイン・写真・歩数などの個人データを自動で集め、**旅行という単位**で束ねて、
地図と時系列からなる読み物 — 旅行記 — を生成するサービス。

> **旅行から帰ってきたら、旅行記が9割できている。**

現在は**設計フェーズ**であり、実装コードはまだ無い。

## ドキュメント

| ファイル | 内容 |
|---|---|
| [docs/concept.md](docs/concept.md) | 解く課題、既存 OSS との差別化、設計の2本柱、非目標 |
| [docs/research.md](docs/research.md) | 既存 OSS 調査、データソース候補と各々の制約 |
| [docs/data-model.md](docs/data-model.md) | Observation / Segment / Trip / Correction / Place の定義 |
| [docs/features.md](docs/features.md) | 機能一覧（MVP / 第2段階 / 第3段階）と受け入れ条件 |
| [docs/decisions.md](docs/decisions.md) | 設計判断とその理由 |

## 想定するデータソース

**MVP** — GPS（dawarich 形式 push）、Swarm チェックイン、写真（JPG/HEIC の EXIF）、KML、歩数（Health Connect）

**以降** — 予約メール、逆ジオコーディング、過去天気、交通系IC 履歴、フライト情報、支出データ

## 次のステップ

設計が固まり次第、技術スタック（言語・DB・地図ライブラリ）と実装計画を検討する。
