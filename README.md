# travelmap

GPS 軌跡・チェックイン・写真・歩数などの個人データを自動で集め、**旅行という単位**で束ねて、
地図と時系列からなる読み物 — 旅行記 — を生成するサービス。

> **旅行から帰ってきたら、旅行記が9割できている。**

現在は**設計フェーズ**であり、実装コードはまだ無い。

## ドキュメント

開発は2つの層に分かれている（D-14）。

| | 範囲 | 文書 |
|---|---|---|
| **Phase 0** | Dawarich 互換 API サーバ。GPS の受信・保存・閲覧。iPhone アプリが繋がる | [TODO.md](TODO.md) |
| **Phase 1 以降** | 旅行タイムライン層。写真・チェックイン・歩数の重ね合わせ、Trip 検出、訂正、旅行記の生成 | `docs/` |

両者の接続点と食い違いの解決は [docs/roadmap.md](docs/roadmap.md) にある。

| ファイル | 内容 |
|---|---|
| [docs/roadmap.md](docs/roadmap.md) | **Phase 0 と Phase 1 の関係、食い違いの解決** |
| [docs/concept.md](docs/concept.md) | 解く課題、既存 OSS との差別化、設計の2本柱、非目標 |
| [docs/research.md](docs/research.md) | 既存 OSS 調査、データソース候補と各々の制約 |
| [docs/data-model.md](docs/data-model.md) | Observation / Segment / Trip / Correction / Asset / Place の定義 |
| [docs/photos.md](docs/photos.md) | 写真データの扱い（3層分離、取り込み経路、EXIF の落とし穴） |
| [docs/features.md](docs/features.md) | 機能一覧（MVP / 第2段階 / 第3段階）と受け入れ条件 |
| [docs/tech-stack.md](docs/tech-stack.md) | リソース予算、言語比較、言語非依存で決まっている構成 |
| [docs/decisions.md](docs/decisions.md) | 設計判断とその理由 |

## 想定するデータソース

**MVP** — GPS（**dawarich 互換の受信エンドポイントを自前実装**。Dawarich サーバは起動しない）、
Swarm チェックイン（webhook）、写真（ローカル / NAS のディレクトリをスキャン）、KML、歩数（Health Connect）

**以降** — 予約メール、逆ジオコーディング、過去天気、交通系IC 履歴、フライト情報、支出データ

## 次のステップ

**Phase 0 の Milestone A（Step 1: ツールチェインと CI）から着手する。**
詳細は [TODO.md](TODO.md)。

Phase 1 に残る保留事項は [docs/decisions.md](docs/decisions.md) の「未決定」を参照。
