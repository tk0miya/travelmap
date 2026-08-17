# travelmap — Dawarich 互換 API サーバ 開発プラン

## 目的

[Dawarich](https://github.com/Freika/dawarich) 互換の Web API サーバを Go で実装する。

本家 Dawarich は Rails + PostgreSQL/PostGIS + Sidekiq + Redis のマルチコンテナ構成であり、
個人利用にはランタイムのフットプリントが大きい。本プロジェクトでは
**静的リンクされた単一バイナリ + SQLite ファイル 1 個**で動作する軽量な互換サーバを目指す。

**最終ゴール**: Dawarich iPhone アプリの接続先として本サーバを指定し、位置情報の記録と閲覧ができること。

その後、**独自の Web UI を構築する予定**（Stage 7）。本家のブラウザ画面の移植ではなく、
本サーバの API の上に自前で作る。API を先に固め、
**UI 専用のデータ取得 API は増やさず既存の `/api/v1` を再利用する**方針とする
（ログインやセッションなどブラウザ固有の経路は Stage 7 で追加する）。

### 非目標

以下は本プロジェクトの対象外とする。

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
| データストア | SQLite（`modernc.org/sqlite`、CGO 不要） | 静的バイナリ 1 個で完結。DB プロセス不要でフットプリント最小。実測で十分な性能（「データストアの検証」節） |
| ストア抽象化 | リポジトリ層を interface で分離 | 将来 PostgreSQL 実装を追加できる余地を残す |
| インデックス | `points(user_id, timestamp)` の 1 本 | アプリのクエリはまず user と期間で絞る。緯度経度インデックスは実装対象に矩形範囲検索が無いため当面張らない（同節） |
| 距離計算 | 集計は SQL 内の Haversine、単発の計算は `internal/geo`（Go） | 全期間の集計を Go 側に持ち出すと行の転送だけで時間を使う。SQL の数学関数が使えることは実測で確認済み。**式が 2 実装になるため、地球半径の定数を共有し、両者が一致することをテストで担保する** |
| 統計 | `daily_stats` 事前集計テーブル + 取り込み時の差分更新 | オンデマンド計算では実用にならない（同節） |
| HTTP | `net/http` + `github.com/go-chi/chi/v5` | Web UI 追加後を見据えた選定（「Web UI を見据えた選定理由」節） |
| 互換範囲 | モバイルアプリ用サブセット | 上記「非目標」を除いた範囲 |
| ユーザー管理 | CLI で発行。`auth/login` は実装、`auth/register` は環境変数で任意有効化、2FA 非対応 | 自己ホスト前提 |
| 非同期処理 | goroutine + SQLite のジョブテーブル | Sidekiq/Redis 相当を持たずプロセス 1 個を維持 |
| 逆ジオコーディング | 既定 OFF。任意で Nominatim/Photon の URL を設定 | 外部サービスへの依存を必須にしない |

これらは着手時点の既定であり、実装中に妥当でないと判明した場合は本ファイルを更新した上で変更してよい。

## データストアの検証（SQLite で足りるか）

「位置情報を扱うのに SQLite で足りるか、PostGIS が要るか」を実測で確認した（2026-08-17）。
環境: `modernc.org/sqlite` v1.56.0 / SQLite 3.53.3、**200 万点**（1 ユーザー・5 年分相当）、WAL。
数値は着手時点のスナップショットであり、再現用スクリプトは残していない
（ドライバや Go を大きく更新して判断を見直したくなったら、この節の条件で測り直す）。

### 使える機能

| 機能 | 可否 | 備考 |
| --- | --- | --- |
| R\*Tree | 使える | 2 次元 bbox 検索用の仮想テーブル |
| geopoly | 使える | ポリゴン内外判定。国境データによる国名付与に使える |
| `sin` / `cos` / `asin` / `radians` / `pow` | 使える | **SQL 内で Haversine を書ける**（東京〜大阪 402.8 km を検算） |
| ウィンドウ関数 `lag()` | 使える | 連続する点の距離差分に必須 |
| WAL / `busy_timeout` | 使える | 読み込み並行性 |

pure Go ドライバでも R\*Tree と geopoly が同梱されており、CGO や SpatiaLite は不要。

### 実測値（200 万点 / DB 209 MB = 104 バイト/点）

いずれも `(user_id, timestamp)` と `(user_id, latitude, longitude)` の
2 インデックスを張った状態での計測（後者は結論 3 のとおり本番では張らない）。

| クエリ | 素朴な実装 | 対策後 |
| --- | --- | --- |
| `GET /points` 1 日分の 1 ページ目（`per_page=100`） | **0.3 ms** | — |
| `GET /points` 1 ヶ月分（32,401 件） | **3.6 ms** | — |
| bbox 検索（186,272 件ヒット） | **173 ms** | R\*Tree で 51 ms（下記 3 のとおり本番では張らない） |
| `GET /points/tracked_months` | 1,448 ms | **0.64 ms** |
| `GET /stats` 全期間の走行距離 | 11,183 ms | **2.29 ms** |
| `GET /stats` 年別集計 | 2,745 ms | **2.29 ms**（同上に含む） |
| 100 点バッチ挿入 + 統計更新 | — | **5.74 ms** |
| `daily_stats` の全再構築（`recalculate`） | — | **15.6 s** |

### ここから導かれる結論

1. **アプリが最も多く叩くクエリ（期間指定の点取得）は 0.3〜4 ms で、SQLite で全く問題ない。**
   位置情報の履歴は本質的に「時系列」ワークロードであり、まず `user_id` と期間で絞る。
   絞った後は 1 日分なら約 1,100 件、1 ヶ月分でも約 3 万件しか残らないので、
   幾何計算はどのみちインデックスの出番がない。
2. **遅かったのは空間クエリではなく全期間の集計だった。** これは DB エンジンの問題ではなく設計の問題で、
   PostgreSQL に替えても同じように遅い。本家 Dawarich が `stats` テーブルと
   `POST /api/v1/recalculations` を持っているのは、まさにこの理由と考えられる。
   → **`daily_stats` を持ち、取り込みと同じトランザクションで影響を受けた日を差分更新する。**
   これで `/stats` は 2.3 ms になる。挿入と統計更新を合わせても 1 バッチ（100 点）6 ms 未満。

   **更新方式は「影響を受けた日の行を丸ごと作り直す」**。値の加減算にはしないこと。
   `countries` / `cities` は集合なので、点を 1 つ消したときにその国・都市が他の点にも
   残っているかはその日を走査しないと判定できず、加減算方式では要素が減らずに
   `/stats` が過大な値を返し続ける。
   1 日分の点は約 1,100 件なので、作り直しでも 1 バッチ 6 ms 未満に収まっている。

   **作り直しの入力は「その日の点 + 直前の点（前日以前でもよい）」**。
   その日の先頭点の区間距離は前日以前の最終点との距離なので、
   その日の点だけでは計算できない。ここを落とすと日をまたぐ移動が全て `km` から消える。

   更新対象は「当日」だけでは閉じない。連続する 2 点の区間距離は
   **後ろの点が属する日に計上する**と決めるため、ある点を挿入・更新・削除すると
   その点の日に加えて、**時系列で次に来る点が属する日**の `km` も変わる。
   実装は「**対象の点が属する日と、時系列で次に来る点が属する日**」を更新すること
   （前の点の日は変わらない。`prev → P` の距離は P の日に計上されているため）。
   点の座標や時刻を更新して所属日が変わる場合は、変更前後それぞれについて同じ 2 日を取る。
   端末を止めていた期間があると次の点は数日先になりうるので「翌日」で固定してはいけない。
   遅延到着・順不同のバッチも同様に扱う。

   **時間差が `TRAVELMAP_TRACK_BREAK_MINUTES`（既定 30）を超える区間は距離に計上しない。**
   端末を止めていた期間や飛行機移動の 2 点間直線距離をそのまま足すと、
   `/stats` の総距離が実態とかけ離れる。Stage 4 のトラック分割にも同じ値を使い、
   「トラック内の移動距離の合計」という一貫した意味にする。

   これは**サーバ側の設定**であり、`settings/mobile` の `track_break`（端末がトラックを
   切る間隔のアプリ設定）とは別物。混同して後者を集計に使うと、ユーザーがアプリで
   設定を変えるたびに過去の集計値の意味が変わってしまう。
   `TRAVELMAP_TIMEZONE` と同じく、変更したら `travelmap recalculate` が必要。
   本家がどう計算しているかは仕様書に書かれていないため、
   Stage 3 で実機の表示と突き合わせて、ずれるようなら見直す。

   カラムは `user_id`, `day`, `points`, `reverse_geocoded_points`, `km`, `countries`, `cities`。
   主キーは `(user_id, day)` の複合。
   **その日の点が 0 件になったら行ごと削除する**（0 のまま残すと `tracked_months` が
   点の無い月を返し続け、`recalculate` による再構築結果とも一致しなくなる）。

   **`day` を切るタイムゾーンは `TRAVELMAP_TIMEZONE`（既定 `UTC`）で決める。**
   `day` は主キーの構成要素なので、後から変えると全ユーザーの `daily_stats` 再構築
   （上表のとおり 200 万点で約 16 秒）が必要になる。日本で使うなら `Asia/Tokyo` を設定しないと、
   0〜9 時の移動が前日に計上され `/stats` の月次・年次と `tracked_months` がアプリの表示とずれる。

   `/stats` の 5 つの合計値（`totalDistanceKm`, `totalPointsTracked`, `totalReverseGeocodedPoints`,
   `totalCountriesVisited`, `totalCitiesVisited`）はすべてこの表だけで出せるようにする
   （`countries` / `cities` はその日に訪れた国・都市名の JSON 配列。全期間の集計時に和集合を取る）。
   逆ジオコーディングは既定 OFF なので、有効化するまで両方とも空配列となり、
   `/stats` はこれらを 0 で返す。本家に合わせるにはユーザーが逆ジオコーダを設定する必要がある。
3. **bbox 検索用のインデックスは当面入れない（R\*Tree も緯度経度の複合インデックスも）。**
   R\*Tree は 173 ms → 51 ms になるが構築に 58 秒かかりストレージも増える。
   そもそも**実装対象のエンドポイントに矩形範囲を取るものが無い**
   （`GET /points` のパラメータは `start_at` / `end_at` / `page` / `per_page` / `order` のみ）。
   使うクエリが無いインデックスは挿入コストとストレージだけを払うことになる。
   矩形範囲検索が必要になったら、その時点でどちらを張るか決める。

### PostgreSQL / PostGIS に乗り換えるべき条件

現時点では該当しない。以下のいずれかが現実になったら、`internal/store` の interface に
PostgreSQL 実装を足して切り替える（そのためにストアを抽象化しておく）。

- 複数ユーザーが常時同時に書き込む（SQLite の書き込みは常に単一）
- 外部の逆ジオコーダに頼らず、自前の境界データで kNN や複雑な空間結合を行う
- H3 ヘックス集計 / fog of war を実装する（いずれも非目標）
- DB が数十 GB 規模になる（上記実測から、1,000 万点でも約 1 GB）

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
- `POST /api/v1/points/reapply_anomaly_filter` と
  `GET /api/v1/settings/transportation_recalculation_status` — 異常値フィルタと移動手段推定。
  **どちらの機能自体も実装しない**ため、再計算のトリガと進捗確認も不要
  （トラックの `dominant_mode` は `null` を返す）。
- `POST /api/v1/recalculations` — `daily_stats` の再構築は必要だが、
  **CLI（`travelmap recalculate`）で行い API としては公開しない**。
  自己ホストで再構築を要するのはインポート後・不整合時・`TRAVELMAP_TIMEZONE` や
  `TRAVELMAP_TRACK_BREAK_MINUTES` の変更時に限られ、いずれも運用者が手元で叩ける。
  アプリから促す必要が実際に出てきたら再検討する。
- `GET /api/v1/countries/borders` — 国境ポリゴンの GeoJSON（数 MB の静的データ配信）。
  国境の描画は地図タイル側に任せる想定のため、Stage 7 の Web UI でも配信しない。
  Web UI の描画方針が固まった時点で再検討する。`countries/visited_cities` のみ Stage 6 で扱う。
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

**下限は Go 1.25**（`modernc.org/sqlite` v1.56.0 の `go.mod` が `go 1.25.0` を要求する）。
ただし**実際に使うのは着手時点の最新安定版**とする。2026-08-17 現在は **go1.26.6**
（1.27 は rc3）で、Go はサポートが最新 2 リリースのみのため、1.27 が GA になった時点で
1.25 はサポート外に落ちる。下限をそのまま入れると、CI の `govulncheck` が toolchain の
脆弱性を報告した時にバージョンを上げる以外の手が無くなる。

着手時に <https://go.dev/dl/> で最新安定版を確認し、`go.mod` の `go` ディレクティブと
CI の `actions/setup-go` をそれに揃える。

### ライブラリ

依存は最小限に抑える。

| 用途 | パッケージ | 備考 |
| --- | --- | --- |
| ルーティング | `github.com/go-chi/chi/v5` | 選定理由は「Web UI を見据えた選定理由」節 |
| SQLite ドライバ | `modernc.org/sqlite` | pure Go。CGO 不要。Go 1.25 以上を要求 |
| パスワードハッシュ | `golang.org/x/crypto/bcrypt` | |
| マイグレーション | `github.com/pressly/goose/v3`（または `embed` + 自前 migrator） | |
| テストの差分表示 | `github.com/google/go-cmp` | |
| 設定 | 追加依存なし（環境変数を読む薄い loader を自前実装） | |
| ロギング | 標準 `log/slog` | |

### Web UI を見据えた選定理由

Web UI を後から追加する予定があるため、その時点で作り直しにならない選択をしておく。
**いま決める必要があるのは router だけ**で、UI 用のライブラリは Stage 7 まで入れない。

**なぜ chi か（標準 `net/http.ServeMux` ではなく）**

Go 1.22 以降の `ServeMux` はメソッドとワイルドカードを書けるので、API サーバ単体なら標準で足りる。
ただし Web UI が加わると **認証方式が 2 系統になる**。

- `/api/v1/*` — `api_key` クエリ / Bearer トークン（モバイルアプリ）
- `/*` — Cookie セッション + CSRF（ブラウザ）

`ServeMux` には**プレフィックス単位で別のミドルウェアチェーンを掛ける仕組みが無い**ため、
これを自前で書くことになる。chi の `Route` / `Group` はまさにこの用途で、
しかも全体が `http.Handler` 準拠なので後から何を足しても壊れない。
chi v5.3.1 の `go.mod` には `require` が 1 つも無く、**依存は増えない**（確認済み）。

```go
r := chi.NewRouter()
r.Route("/api/v1", func(r chi.Router) {
    r.Use(auth.APIKey)      // Bearer / api_key
    ...
})
r.Group(func(r chi.Router) {
    r.Use(session.Load, csrf.Protect)  // ブラウザ
    r.Handle("/*", webui.Handler())
})
```

**Stage 7 で追加を検討するもの（いまは入れない）**

| 用途 | 候補 | 補足 |
| --- | --- | --- |
| セッション | `github.com/alexedwards/scs/v2` | SQLite ストアあり。`gorilla/sessions` より保守が活発 |
| CSRF | 標準 `net/http.CrossOriginProtection` | **Go 1.25 で標準ライブラリに入った**（`Sec-Fetch-Site` ベース）。外部依存が要らないか着手時に確認する |
| テンプレート | `github.com/a-h/templ` または標準 `html/template` | templ は型安全だがコード生成ステップが増える |
| 画面更新 | htmx | Node のビルドチェーンを持ち込まずに済む |
| 地図描画 | MapLibre GL JS または Leaflet | ここだけは JS が避けられない。CDN ではなく vendor して `embed.FS` に入れ、単一バイナリを保つ |

SPA（React 等）にする場合も、ビルド成果物を `embed.FS` に入れれば単一バイナリは維持できる。
その場合は Node のビルドチェーンが必要になる点だけがトレードオフで、
**router の選択はどちらでも変わらない**。

**未決: ブラウザから `/api/v1` を叩くときの認証**

「UI 専用のデータ取得 API は増やさない」と決めた以上、ブラウザも `/api/v1/points` 等を叩くが、
上の構成では `/api/v1` は Bearer / `api_key` のみでセッション Cookie は `/*` 側にある。
Stage 7 着手時に決める。現時点の第一候補は **(a)**。

- **(a) `/api/v1` のミドルウェアがセッション Cookie も受理する** — UI から素直に fetch できる。
  Cookie を受ける以上 `/api/v1` にも CSRF 対策が要るが、Go 1.25 の `CrossOriginProtection` は
  サーバ全体に一括で掛けられるので追加コストは小さい。
- (b) UI 側はサーバ内でハンドラ / ストアを直接呼ぶ — 認証の分離は保てるが、
  「API を再利用する」の意味が HTTP API の再利用から実装の再利用に変わる。
- (c) ログイン時に api_key を UI に渡して Bearer で叩く — XSS 時に API キーが漏れるため推奨しない。

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
cmd/travelmap/            エントリポイント (serve / user create / migrate / recalculate)
internal/config/          環境変数ローダ
internal/httpapi/         ルーティング・ミドルウェア・ハンドラ
internal/httpapi/dto/     Dawarich 互換の JSON 構造体（互換性をここに集約する）
internal/auth/            API キー発行・検証、bcrypt
internal/ingest/          点の投入・更新・削除と daily_stats 差分更新（全経路がここを通る）
internal/store/           リポジトリ interface
internal/store/sqlite/    SQLite 実装 + マイグレーション (embed)
internal/model/           ドメインモデル (User, Point, Track, Visit, Stat)
internal/geo/             Haversine（単発計算）、トラック分割の判定ロジック
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

- [ ] （前提作業・差分には現れない）開発環境の Go を最新安定版にする。
      この環境は 1.24.7 で下限の 1.25 にも届かない。`GOTOOLCHAIN=auto` なら自動取得されるが
      明示的に上げておく（「言語・ツールチェイン」節）
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

- [ ] `points` スキーマを確定（本家 point の全フィールドを網羅）。
      インデックスは `(user_id, timestamp)` の 1 本
      （**無いと `GET /points` の期間フィルタが全件走査になる**。
      緯度経度側は「データストアの検証」節のとおり張らない）
- [ ] GeoJSON Feature パーサ（`internal/httpapi/dto`）
- [ ] `POST /api/v1/points`
- [ ] `POST /api/v1/overland/batches`
- [ ] 重複排除（同一 user × timestamp）
- [ ] バッチ挿入をトランザクション化
- [ ] `daily_stats` テーブルと、取り込みと同じトランザクション内での更新
      （影響を受けた日**だけ**を対象に、その日の行を points から作り直す。値の加減算にはしない）。
      **Stage 3 の `/stats` を実用速度にするために、記録側で作っておく必要がある**
      （カラム定義・更新対象の日・区間距離の帰属は「データストアの検証」節）
- [ ] `TRAVELMAP_TIMEZONE` で `day` を切り、`TRAVELMAP_TRACK_BREAK_MINUTES` で
      距離に計上する区間を決める（既定値と影響は「データストアの検証」節）。
      **どちらも変更したら `travelmap recalculate` が必要**な旨を README に書く
- [ ] `travelmap recalculate` サブコマンド（`daily_stats` を points から再構築する。
      インポートや不整合時の復旧用）
- [ ] **点を変更する経路をすべて 1 つの ingest / mutation 層に通す。**
      `daily_stats` の更新箇所が散らばると必ず取りこぼす（Stage 3 の更新・削除、
      Stage 6 のインポート・owntracks/traccar・逆ジオコーディングワーカーも同じ層を通す）
- [ ] `internal/geo` の Haversine と SQL 内の Haversine が同一入力で一致することを検証するテスト
      （地球半径の定数は Go 側から SQL に渡し、2 箇所に literal を置かない）
- [ ] **`TRAVELMAP_TIMEZONE=Asia/Tokyo` のテスト。** 列挙した他のケースは既定の `UTC` でも通るため、
      TZ 変換を忘れても検出できない。UTC 基準では前日に落ちる時刻（例 00:30 JST）の点が
      当日の行に計上されることを確かめる
- [ ] `TRAVELMAP_TRACK_BREAK_MINUTES` の境界テスト。ちょうど 30 分の区間は**計上する**側
      （`>` と `>=` の取り違えを拾う）
- [ ] **日をまたぐ区間距離の期待値を直接固定するテスト。**
      一致比較だけでは不十分で、差分更新と `recalculate` の両方が同じ取りこぼしをすると
      両者は一致したまま通ってしまう（前日最終点と当日先頭点の距離が `km` に入ることを
      期待値で確かめる）
- [ ] 上記に加えて、差分更新と `recalculate` の一致を検証するテスト。同じ points 集合に対して
      「取り込みごとの更新を積み重ねた `daily_stats`」と「`recalculate` で全再構築した
      `daily_stats`」が一致すること。
      日付境界（前日最終点と当日先頭点の距離）、順不同・遅延到着のバッチ、
      **前後の点が数日空いているケース**（端末を止めていた期間の前後。
      `TRAVELMAP_TRACK_BREAK_MINUTES` 超の区間が距離に計上されないことも確認する）、
      **その日の点が全部消えて行が削除されるケース**、
      **その日の一部の点だけを削除・更新して `countries` / `cities` が減るケース**を含めること

**完了条件**: アプリでトラッキングを開始すると実機の位置が DB に入り、点の件数が増えていく。
あわせて、点を投入すると `daily_stats` の該当日が更新され、
その後 `travelmap recalculate` を実行しても同じ値になること（差分更新と再構築の一致）。

### Stage 3: 閲覧 — アプリで自分の軌跡が見える

- [ ] `GET /api/v1/points`（期間フィルタ・ページング・`X-Current-Page` / `X-Total-Pages`）
- [ ] `PATCH /api/v1/points/{id}`（body は `{"point": {...}}` ラップ）, `DELETE /api/v1/points/{id}`
- [ ] `DELETE /api/v1/points/bulk_destroy`（body は `{"point_ids": [...]}`）
- [ ] 上記の更新・削除で、**影響を受けた日の `daily_stats` を同一トランザクションで再計算する**。
      点を消したのに `/stats` が古い距離を返し続ける状態を作らないこと
- [ ] `GET /api/v1/points/tracked_months`（`daily_stats` から引く）
- [ ] `GET /api/v1/stats`（`daily_stats` を集計。camelCase を厳守）
      **points を直接集計しないこと**（理由と実測値は「データストアの検証」節）

**完了条件**: アプリの地図に過去の点が表示され、統計画面に距離と点数が出る。

> **この Stage 3 到達時点で「iPhone アプリから位置情報の記録・閲覧ができる」という要求を満たす。**
> Stage 4 以降はアプリの他の画面を成立させるための追加作業。

### Stage 4: トラック / 訪問 / タイムライン

- [ ] 点列をトラックに分割するロジック（`TRAVELMAP_TRACK_BREAK_MINUTES` 分の無活動で分割）を
      バックグラウンドジョブ化。**`settings/mobile` の `track_break` ではない**（「データストアの検証」節）
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
      **取り込みは Stage 2 と同じ ingest 層を通し、`daily_stats` を更新すること**
- [ ] 逆ジオコーディング（Nominatim / Photon、レート制限付きワーカー）。
      点の投入後に非同期で走るため、**完了時に対象日の `daily_stats` の
      `countries` / `cities` / `reverse_geocoded_points` を更新する**
      （更新しないと `/stats` の該当する値が 0 のまま。「データストアの検証」節）
- [ ] `POST /api/v1/auth/register`（環境変数で有効化）
- [ ] `POST /api/v1/owntracks/points`, `POST /api/v1/traccar/points`
- [ ] `GET /api/v1/countries/visited_cities`
- [ ] バックアップ（`VACUUM INTO`）
- [ ] `GET /metrics`、構造化アクセスログ

### Stage 7: Web UI

API が固まってから着手する。ライブラリ候補と選定理由は「Web UI を見据えた選定理由」節を参照。

- [ ] Cookie セッション + CSRF。`/api/v1` をブラウザからどう認証するかは
      「未決: ブラウザから `/api/v1` を叩くときの認証」の結論に従う（第一候補は (a)）
- [ ] `travelmap user create` 済みのアカウントでログインできるログイン画面
- [ ] 地図画面（期間指定で points / tracks を描画）。既存の `GET /api/v1/points`・`/tracks` を再利用し、
      UI 専用の API は増やさない
- [ ] 統計画面（`daily_stats` を利用）
- [ ] 設定画面。インポート画面は Stage 6 の `/api/v1/imports` を実施した場合のみ
- [ ] 静的アセットを `embed.FS` に入れ、単一バイナリを維持する

**完了条件**: ブラウザからログインして地図上に自分の軌跡が表示され、
バイナリ 1 個 + SQLite ファイル 1 個のままデプロイできる。

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
