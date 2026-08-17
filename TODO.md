# travelmap — Dawarich 互換 API サーバ 開発プラン

## 目的

[Dawarich](https://github.com/Freika/dawarich) 互換の Web API サーバを Go で実装する。

本家 Dawarich は Rails + PostgreSQL/PostGIS + Sidekiq + Redis のマルチコンテナ構成であり、
個人利用にはランタイムのフットプリントが大きい。本プロジェクトでは
**静的リンクされた単一バイナリ + SQLite ファイル 1 個**で動作する軽量な互換サーバを目指す。

**最終ゴール**: Dawarich iPhone アプリの接続先として本サーバを指定し、位置情報の記録と閲覧ができること。

### 非目標

以下は本プロジェクトの対象外とする。

- Web UI（本家のブラウザ向け画面）
- Immich / Photoprism 連携、写真関連 API
- 課金・サブスクリプション関連 API
- 家族共有（Families）
- H3 ヘックスマップ / フォグオブウォー
- Areas, Places, Notes, Tags, Digests, Insights

## 参照仕様

Dawarich の OpenAPI 仕様を唯一の互換性の根拠とする。

- 取得元: `https://raw.githubusercontent.com/Freika/dawarich/master/swagger/v1/swagger.yaml`
- 取得日: 2026-08-17
- fingerprint: 5680 行 / `sha256:a16411a389e0130d9e0b04b54cfc80726c234b8a017cc76d9d921bfc91adc89a`

本家仕様は継続的に変更されるため、仕様を再取得した際は上記 fingerprint を更新すること。
稼働中の Dawarich インスタンスがあれば `/api-docs` からも同じ仕様を参照できる。

## 技術判断

| 項目 | 決定 | 理由 |
| --- | --- | --- |
| データストア | SQLite（`modernc.org/sqlite`、CGO 不要） | 静的バイナリ 1 個で完結。DB プロセス不要でフットプリント最小 |
| ストア抽象化 | リポジトリ層を interface で分離 | 将来 PostgreSQL 実装を追加できる余地を残す |
| 空間検索 | 緯度経度の B-Tree インデックス + Haversine を Go 側で計算 | PostGIS 不要。個人利用（数百万点規模）なら十分 |
| HTTP | `net/http` + `github.com/go-chi/chi/v5` | 依存がほぼゼロで標準 `http.Handler` 互換 |
| 互換範囲 | モバイルアプリ用サブセット | 上記「非目標」を除いた範囲 |
| ユーザー管理 | CLI で発行。`auth/login` は実装、`auth/register` は環境変数で任意有効化、2FA 非対応 | 自己ホスト前提 |
| 非同期処理 | goroutine + SQLite のジョブテーブル | Sidekiq/Redis 相当を持たずプロセス 1 個を維持 |
| 逆ジオコーディング | 既定 OFF。任意で Nominatim/Photon の URL を設定 | 外部サービスへの依存を必須にしない |

これらは着手時点の既定であり、実装中に妥当でないと判明した場合は本ファイルを更新した上で変更してよい。

## Dawarich API 互換性メモ

実装前に必ず確認すべき、本家仕様の癖。

### 認証

- `api_key` クエリパラメータ **または** `Authorization: Bearer {api_key}` ヘッダ。
- 仕様上はエンドポイントごとにどちらか一方しか記載がない（points / stats / tracks / settings はクエリ、users/me・visits はヘッダ）が、
  **実装側は全エンドポイントで両方を受け付ける**こと。アプリがどちらを使うかは不明。

### エンドポイント個別

- **`GET /api/v1/health`** — 認証不要。レスポンスヘッダ `X-Dawarich-Response`（未認証は `Hey, I'm alive!`、認証済みは `Hey, I'm alive and authenticated!`）と `X-Dawarich-Version` が **required**。body は `{"status":"ok"}`。アプリのサーバ URL 検証がここを通ると**推測**されるため最優先で実装する（要実機確認。ただし外れても health 自体は必要なので手戻りは無い）。
- **`POST /api/v1/auth/login`** — body `{email, password}` → 200 で `{user_id, email, api_key, status, plan, subscription_source, active_until}`。2FA 有効時は 202 + `challenge_token`（本プロジェクトでは 200 のみ返す）。
- **`POST /api/v1/points`** / **`POST /api/v1/overland/batches`** — どちらも body は `{"locations": [GeoJSON Feature, ...]}`。Feature の `properties` に `timestamp`(ISO8601), `horizontal_accuracy`, `vertical_accuracy`, `altitude`, `speed`, `speed_accuracy`, `course`, `course_accuracy`, `battery_state`, `battery_level`, `wifi`, `track_id`, `device_id` 等。**成功時のステータスコードが異なる**（points は 200、overland は 201）。
- **`GET /api/v1/points`** — `start_at` / `end_at` / `page` / `per_page` / `order`。レスポンスヘッダ `X-Current-Page`, `X-Total-Pages` が必要。body は約 30 フィールドを持つ point オブジェクトの配列。
- **`GET /api/v1/stats`** — このエンドポイントだけ **camelCase**（`totalDistanceKm`, `totalPointsTracked`, `totalReverseGeocodedPoints`, `totalCountriesVisited`, `totalCitiesVisited`, `yearlyStats[].monthlyDistanceKm.january` …）。他は snake_case なので取り違えないこと。
- **`GET /api/v1/points/tracked_months`** — `[{"year": 2024, "months": ["Jan", "Feb", ...]}]`。月は 3 文字の英語省略名。
- **`GET/PATCH /api/v1/settings/mobile`** — GET は `{settings: {...}, updated_at, status}` でラップする。PATCH は **仕様内で schema と example が矛盾している**（schema はトップレベル直置き、example は `{"settings": {...}}` でラップ）。どちらで送られても壊れないよう、**実装は両形式を受理する**こと。片方だけを前提にすると、もう一方の形式で送られた際に全フィールドが無視され、エラーにもならず設定同期が黙って壊れる。
- **`GET /api/v1/tracks`** — GeoJSON `FeatureCollection`（LineString）。properties は `id`, `color`, `start_at`, `end_at`, `distance`（メートル）, `avg_speed`（km/h）, `duration`（秒）, `dominant_mode`, `dominant_mode_emoji`。
- **`GET /api/v1/timeline`** — `start_at` / `end_at` 必須、**期間は最大 31 日**。レスポンスは `{days: [...]}`。
- **リクエスト body のラップに一貫性がない** — `PATCH /api/v1/points/{id}` は `{"point": {"latitude": ..., "longitude": ...}}` とラップされ、`DELETE /api/v1/points/bulk_destroy` は `{"point_ids": [...]}` 直置き。エンドポイントごとに仕様を確認すること。

### iOS アプリについて

`dawarich-app/dawarich-ios` は**空リポジトリ**でありアプリは非公開。
そのため実際に呼ばれるエンドポイント・必須フィールド・呼び出し順序は仕様書からは確定できない。
Stage 1 でリクエストログ用のミドルウェアを入れ、実機の通信を観測しながら埋めていく方針をとる。

## 対象外とするエンドポイント

**実装するエンドポイントは「開発ステップ」節のチェックリストが唯一の正**とする。
一覧を別に持つと二重管理になり更新漏れで実態とずれるため、ここには対象外の判断とその根拠だけを置く。

仕様に存在するエンドポイントは、「非目標」節のカテゴリに該当するもの（Areas, Places, Notes, Tags,
Digests, Insights, Families, Immich, Photos, Maps/hexagons など）を除き、
以下に挙がっていなければ開発ステップ側に載っているはず。

- `GET /api/v1/points/{id}` と `GET /api/v1/visits/{id}` は**仕様に存在しない**（どちらも `patch` と `delete` のみ）。
  実機ログで叩かれることが判明したら、その観測結果を根拠として追加する。
- `POST /api/v1/visits`, `PATCH/DELETE /api/v1/visits/{id}`, `POST /api/v1/visits/merge`,
  `POST /api/v1/visits/bulk_update` — 訪問の編集系。閲覧のみを目標とするため当面は対象外。
- `GET /api/v1/plan`, `POST /api/v1/subscriptions/callback` — 課金・サブスク関連（非目標）。
- `POST /api/v1/users/exist` — 本家 Cloud の Subscription Manager 用内部エンドポイント（非目標）。
- `GET /api/v1/demo_data`, `POST /api/v1/demo_data`, `DELETE /api/v1/demo_data` — 本家 Cloud のデモ用（非目標）。
- `POST /api/v1/points/reapply_anomaly_filter`, `POST /api/v1/recalculations`,
  `GET /api/v1/settings/transportation_recalculation_status` — 再計算のトリガと進捗確認。
  本プロジェクトは統計・トラックをオンデマンド計算するため不要（Stage 4 でバックグラウンドジョブを
  入れた結果、進捗表示が必要になったら再検討する）。
- `GET /api/v1/countries/borders` — 国境の GeoJSON（数 MB の静的データ配信）。地図描画はアプリ側の
  責務と見なし対象外。`countries/visited_cities` のみ Stage 6 で扱う。
- `GET /api/v1/locations`, `GET /api/v1/locations/suggestions`, `GET /api/v1/residency` — 場所検索・
  滞在分析。Places 系（非目標）に連なる機能のため対象外。
- `POST /api/v1/auth/otp_challenge`, `GET/POST/DELETE /api/v1/users/me/two_factor`,
  `POST /api/v1/users/me/two_factor/setup`, `POST /api/v1/users/me/two_factor/confirm`,
  `GET /api/v1/users/me/two_factor/backup_codes` — 2FA。自己ホスト前提のため非対応
  （`POST /api/v1/auth/login` は 202 を返さず常に 200 で `api_key` を返す）。
- `POST /api/v1/auth/apple`, `POST /api/v1/auth/google` — ソーシャルログイン。
  **Stage 1 の完了を妨げうる要注意項目**。「リスク・未確定事項」節を参照。

## 開発環境

### 言語・ツールチェイン

**Stage 0 の着手時に、サポート対象の最新安定版を確認してから決めること。**
Go は最新 2 リリースのみをサポートするため、CI に入れる `govulncheck` が
サポート外系列の toolchain 脆弱性を拾った場合、バージョンを上げる以外の対処が無くなる。

この環境にインストールされているのは Go 1.24.7 だが、2026-08 時点では 1.24 系はすでに
サポート範囲を外れている可能性が高い。`go version` と <https://go.dev/dl/> を確認し、
`go.mod` の `go` ディレクティブと CI の `actions/setup-go` を最新安定版に揃える。

### ライブラリ

依存は最小限に抑える。

| 用途 | パッケージ |
| --- | --- |
| ルーティング | `github.com/go-chi/chi/v5` |
| SQLite ドライバ | `modernc.org/sqlite`（pure Go） |
| パスワードハッシュ | `golang.org/x/crypto/bcrypt` |
| マイグレーション | `github.com/pressly/goose/v3`（または `embed` + 自前 migrator） |
| テストの差分表示 | `github.com/google/go-cmp` |
| 設定 | 追加依存なし（環境変数を読む薄い loader を自前実装） |
| ロギング | 標準 `log/slog` |

### 開発ツール

- `golangci-lint` — `.golangci.yml` で errcheck, govet, staticcheck, revive, gosec, bodyclose, sqlclosecheck を有効化
- `gofumpt` — フォーマット
- `govulncheck` — 脆弱性チェック
- `go test ./... -race -cover`

### Makefile

`build` / `test` / `lint` / `fmt` / `run` / `migrate` / `docker` ターゲットを用意する。

### CI

`.github/workflows/go.yml` を追加。ジョブは `build` / `test`（`-race`）/ `lint` / `govulncheck`。

**既存ワークフローの規約を必ず踏襲すること**（`.github/workflows/workflow-lint.yml` 参照）。

- すべての workflow に `permissions:` ブロックを書く
- サードパーティ Action は full commit SHA でピン留めし `# vX.Y.Z` コメントを添える
- `actions/checkout` には `persist-credentials: false` を指定する

あわせて `.github/dependabot.yml` に `gomod` エコシステムを追加する（weekly / `Asia/Tokyo` / cooldown 7 日）。

### コンテナ

- マルチステージ `Dockerfile`。builder で `CGO_ENABLED=0 go build -ldflags="-s -w"` し、`gcr.io/distroless/static` に配置
- `docker-compose.yml` はサーバ 1 コンテナ + SQLite 用 volume のみ

### ディレクトリ構成

```
cmd/travelmap/            エントリポイント (serve / user create / migrate サブコマンド)
internal/config/          環境変数ローダ
internal/httpapi/         ルーティング・ミドルウェア・ハンドラ
internal/httpapi/dto/     Dawarich 互換の JSON 構造体（互換性をここに集約する）
internal/auth/            API キー発行・検証、bcrypt
internal/store/           リポジトリ interface
internal/store/sqlite/    SQLite 実装 + マイグレーション (embed)
internal/model/           ドメインモデル (User, Point, Track, Visit, Stat)
internal/geo/             Haversine、トラック分割、統計集計
api/openapi.yaml          実装範囲だけを抜き出した OpenAPI（参照用）
testdata/golden/          本家レスポンス形状の golden JSON
```

### テスト方針

- ハンドラは `net/http/httptest` + 一時 SQLite を使ったテーブル駆動テスト
- **JSON のキー名・型・命名規則（camelCase / snake_case）を golden ファイルで固定する**。互換性の要はここ
- `go test -race` を CI で回す

## 開発ステップ

1 ステージ = 1 PR を想定。各ステージには「実行して確かめられる完了条件」を置く。

### Stage 0: プロジェクト基盤

アプリケーション機能はまだ持たせない。

- [ ] `go.mod` 作成、ディレクトリ雛形を用意
- [ ] `cmd/travelmap` で `--version` が動く
- [ ] `Makefile`, `.gitignore`（Go 用 + `*.db`, `bin/`）, `.golangci.yml`
- [ ] `CLAUDE.md`（プロジェクト規約: コミットメッセージ言語、レイヤ構成、テスト方針、文書の配置規約）
- [ ] `README.md` の拡充（現状 1 行。プロジェクトの目的、ビルド・起動手順を書く）
- [ ] `.github/workflows/go.yml`（build / test / lint / govulncheck）
- [ ] `.github/dependabot.yml` に `gomod` を追加
- [ ] `Dockerfile`, `docker-compose.yml`

**完了条件**: CI が緑になる。`docker build` が通り、イメージサイズが 30MB 以下。

### Stage 1: 疎通 — アプリがサーバを認識する

- [ ] SQLite ストアとマイグレーション基盤（`users`, `points` テーブル）
- [ ] `travelmap user create --email --password` で user と API キーを発行
- [ ] 認証ミドルウェア（クエリ `api_key` / Bearer 両対応）
- [ ] `GET /api/v1/health`（`X-Dawarich-Response` / `X-Dawarich-Version` ヘッダを含む）
- [ ] `POST /api/v1/auth/login`
- [ ] `GET /api/v1/users/me`
- [ ] リクエストログ用ミドルウェア（`TRAVELMAP_DEBUG_LOG_REQUESTS=1` で有効化）。**実機接続時に叩かれた未実装エンドポイントを洗い出し、本ファイルに追記する**

**完了条件**: iPhone アプリにサーバ URL と API キーを設定して「接続成功」になる。

### Stage 2: 記録 — 位置情報が保存される

- [ ] `points` スキーマを確定（本家 point の全フィールドを網羅）
- [ ] GeoJSON Feature パーサ（`internal/httpapi/dto`）
- [ ] `POST /api/v1/points`
- [ ] `POST /api/v1/overland/batches`
- [ ] 重複排除（同一 user × timestamp）
- [ ] バッチ挿入をトランザクション化

**完了条件**: アプリでトラッキングを開始すると実機の位置が DB に入り、点の件数が増えていく。

### Stage 3: 閲覧 — アプリで自分の軌跡が見える

- [ ] `GET /api/v1/points`（期間フィルタ・ページング・`X-Current-Page` / `X-Total-Pages`）
- [ ] `PATCH /api/v1/points/{id}`（body は `{"point": {...}}` ラップ）, `DELETE /api/v1/points/{id}`
- [ ] `DELETE /api/v1/points/bulk_destroy`（body は `{"point_ids": [...]}`）
- [ ] `GET /api/v1/points/tracked_months`
- [ ] `GET /api/v1/stats`（Haversine で距離集計、camelCase を厳守）

**完了条件**: アプリの地図に過去の点が表示され、統計画面に距離と点数が出る。

> **この Stage 3 到達時点で「iPhone アプリから位置情報の記録・閲覧ができる」という要求を満たす。**
> Stage 4 以降はアプリの他の画面を成立させるための追加作業。

### Stage 4: トラック / 訪問 / タイムライン

- [ ] 点列をトラックに分割するロジック（`track_break` 分の無活動で分割）をバックグラウンドジョブ化
- [ ] `GET /api/v1/tracks`（GeoJSON FeatureCollection）
- [ ] `GET /api/v1/tracks/{id}`, `GET /api/v1/tracks/{track_id}/points`
- [ ] 滞在検出 → `visits` テーブル
- [ ] `GET /api/v1/visits`
- [ ] `GET /api/v1/timeline`（31 日上限のバリデーション込み）

**完了条件**: アプリのタイムライン / トラック画面が破綻せず描画される。

### Stage 5: 設定同期

- [ ] `GET/PATCH /api/v1/settings/mobile`（12 項目、範囲バリデーション付き。PATCH は直置き / `settings` ラップの両形式を受理）
  - `tracking_mode`(precise|significant), `tracking_visits`, `track_visits_independently`, `auto_start`,
    `distance_filter`(1-10000 m), `time_filter`(1-3600 s), `track_break`(1-1440 min), `accuracy`(1-6),
    `show_background_location_indicator`, `upload_automatically`, `upload_all_on_tracking_stop`, `batch_size`(1-1000)
- [ ] `GET/PATCH /api/v1/settings`

**完了条件**: アプリ側の設定変更がサーバに保存され、アプリ再インストール後も復元される。

### Stage 6: 運用・拡張（任意）

- [ ] `POST /api/v1/imports`, `GET /api/v1/imports`, `GET /api/v1/imports/{id}`
      （GPX / GeoJSON / Google Takeout / 本家 Dawarich エクスポート）
- [ ] 逆ジオコーディング（Nominatim / Photon、レート制限付きワーカー）
- [ ] `POST /api/v1/auth/register`（環境変数で有効化）
- [ ] `POST /api/v1/owntracks/points`, `POST /api/v1/traccar/points`
- [ ] `GET /api/v1/countries/visited_cities`
- [ ] バックアップ（`VACUUM INTO`）
- [ ] `GET /metrics`、構造化アクセスログ

## リスク・未確定事項

- **iOS アプリが非公開のため、実際に叩くエンドポイントと必須フィールドは未確定。**
  Stage 1 のリクエストログミドルウェアで実機の通信を観測し、未実装エンドポイントを洗い出す。
- **ソーシャルログインが必須だと Stage 1 が完了しない可能性がある。**
  仕様には `POST /api/v1/auth/apple`（body `{id_token, nonce}`）と `POST /api/v1/auth/google` がある。
  iOS アプリが Sign in with Apple を強制する作りだと、`POST /api/v1/auth/login` だけでは
  「アプリで接続成功」に到達できない。Stage 1 で最初に確認すべき事項。
  必要と判明した場合は、Apple の公開鍵で `id_token` を検証し既存ユーザーに紐づける実装を Stage 1 に追加する
  （自己ホストで新規ユーザーを自動生成するかは別途判断）。
- アプリがサーバの最低バージョンをチェックしている可能性がある。
  `X-Dawarich-Version` には実在する新しめのバージョン文字列（例: `1.10.0`）を返す。要実機確認。
- 未実装エンドポイントは 404 より **空配列 / 空オブジェクト + 200** の方がアプリがクラッシュしにくい可能性がある。
  実機の挙動を見て方針を決める。
