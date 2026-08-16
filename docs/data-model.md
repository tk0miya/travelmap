# データモデル

## 全体構造

```
  ┌─────────────────────────────────────────────┐
  │  Observation   不変・追記のみ                │  ← 生データ層
  │  時刻・位置・ソース・生ペイロード            │
  └─────────────────────────────────────────────┘
                      │ 導出（いつでも捨てて再計算できる）
                      ▼
  ┌─────────────────────────────────────────────┐
  │  Segment       Stay（滞在） / Move（移動）   │  ← 解釈層
  └─────────────────────────────────────────────┘
                      │ 束ね（いつでも捨てて再計算できる）
                      ▼
  ┌─────────────────────────────────────────────┐
  │  Trip          旅行1件                      │
  └─────────────────────────────────────────────┘
                      ▲
  ┌─────────────────────────────────────────────┐
  │  Correction    人手の訂正。再計算で消えない  │  ← 訂正層
  │  Annotation    人が書いた文章               │
  └─────────────────────────────────────────────┘

  ┌─────────────────────────────────────────────┐
  │  Place         正規化された場所マスタ        │  ← 参照層
  │  Asset         写真のメタ・サムネ・原本参照  │
  └─────────────────────────────────────────────┘
```

**最重要の原則**: 解釈層（Segment / Trip）は**いつでも全削除して再構築できる**。
生データ層（Observation）と訂正層（Correction / Annotation）だけが失ってはならないデータである。

---

## Observation（観測）

すべての入力データの共通形式。**一度書いたら変更しない。追記のみ。**

| フィールド | 型 | 説明 |
|---|---|---|
| `id` | ID | |
| `source` | enum | `gps_dawarich` / `swarm_checkin` / `photo_exif` / `kml` / `health_steps` / `booking_mail` / `ic_card` / … |
| `source_record_id` | string | ソース側の一意 ID。**再取り込み時の重複排除に使う** |
| `observed_at` | timestamp | **UTC で保存する** |
| `observed_tz` | string | 観測地点のタイムゾーン（IANA 名）。座標から導出 |
| `lat` / `lon` | float, nullable | 位置。歩数など位置を持たない観測では NULL |
| `accuracy_m` | float, nullable | 位置精度（メートル）。GPS のみ |
| `altitude_m` | float, nullable | |
| `duration_s` | int, nullable | 期間を持つ観測（歩数の集計期間、宿泊など）で使う |
| `payload` | JSON | **ソース固有の生データをそのまま保持する** |
| `ingested_at` | timestamp | 取り込み時刻 |

### 設計判断

**なぜ `payload` に生データを丸ごと持つのか**

取り込み時にはどのフィールドが後で必要になるか分からない。Swarm の venue カテゴリ、
写真の向き・レンズ情報、IC カードの利用種別 — これらは最初のスキーマでは不要に見えるが、
後で「移動手段の推定に使える」と気づく。**そのとき再取り込みできるとは限らない**
（Swarm の webhook は過去分を再送してくれない）。生データは捨てないのが安全側。

**なぜ位置を持たない観測も Observation にするのか**

歩数は位置を持たないが、**時刻で他の観測に紐づく**。「この滞在の間に3,200歩」を出すには
同じ時間軸に載っている必要がある。位置の有無でテーブルを分けると、時刻での突き合わせが二重管理になる。

---

## Segment（セグメント）

Observation から導出される「滞在」と「移動」。**再計算可能。**

### 共通

| フィールド | 型 | 説明 |
|---|---|---|
| `id` | ID | |
| `kind` | enum | `stay` / `move` |
| `started_at` / `ended_at` | timestamp | UTC |
| `tz` | string | 表示に使うタイムゾーン |
| `trip_id` | ID, nullable | 所属する旅行。旅行外の日常なら NULL |
| `confidence` | float | 0.0〜1.0。自動検出の確信度 |
| `derived_from` | ID[] | **根拠となった Observation の ID 列** |
| `computed_at` | timestamp | この導出結果を計算した時刻 |

### Stay（滞在）

| フィールド | 型 | 説明 |
|---|---|---|
| `place_id` | ID, nullable | 紐づく Place |
| `center_lat` / `center_lon` | float | 滞在の代表座標 |
| `radius_m` | float | 滞在範囲の広がり |
| `place_source` | enum | 場所名の**出所**。`swarm_checkin` / `osm_poi` / `booking` / `user_correction` / `unknown` |

### Move（移動）

| フィールド | 型 | 説明 |
|---|---|---|
| `from_stay_id` / `to_stay_id` | ID | 前後の滞在 |
| `mode` | enum, nullable | `walk` / `train` / `car` / `bus` / `flight` / `bicycle` / `ship` / `unknown` |
| `mode_source` | enum | 推定の根拠。`speed_profile` / `step_count` / `ic_card` / `flight_data` / `user_correction` |
| `distance_m` | float | |
| `path` | geometry, nullable | 軌跡。**GPS 欠損区間では NULL または補間フラグ付き** |
| `is_gap` | bool | **観測が存在しない区間かどうか**。飛行機・地下鉄・電池切れ |

### 設計判断

**なぜ `place_source` / `mode_source` を持つのか**

「清水寺」という判定が Swarm チェックイン由来（確実）なのか、GPS + OSM の推定（不確実）なのかで
**ユーザーが疑うべきかどうかが変わる**。UI で「これは推定です」と示せることが訂正のしやすさに直結する。
既存 OSS はここを持たないため、間違いを見つけにくい。

**なぜ `is_gap` を明示するのか**

「観測が無い」ことと「動いていない」ことは全く違う。飛行機で8時間移動したのに
観測が無いから「滞在していた」と誤判定するのが最悪のケース。
**欠損は欠損として1級の状態にして、後から他ソースで埋める対象にする。**

---

## Trip（旅行）

| フィールド | 型 | 説明 |
|---|---|---|
| `id` | ID | |
| `title` | string | 自動生成 or 人が命名。「京都旅行」 |
| `started_at` / `ended_at` | timestamp | UTC |
| `home_tz` | string | 出発地のタイムゾーン。**「何日目」の計算基準** |
| `detection_source` | enum | `auto_distance` / `booking` / `user_created` |
| `confidence` | float | |
| `computed_at` | timestamp | |

### 「何日目」の扱い

日付変更線をまたぐ旅行で「3日目」を素朴に計算すると破綻する。

**方針**: 日の区切りは `home_tz` ではなく**その時点の現地時間の 04:00** を境界とする。
深夜移動が「日をまたいだ」ことにならず、体感と一致する。

---

## Correction（訂正）

**人手の訂正。再計算で消えない。** 本サービスの中核。

| フィールド | 型 | 説明 |
|---|---|---|
| `id` | ID | |
| `target_kind` | enum | `stay` / `move` / `trip` |
| `anchor` | JSON | **後述**。再計算後も対象を特定するための情報 |
| `operation` | enum | `set_place` / `merge` / `split` / `delete` / `set_mode` / `set_trip_boundary` / `exclude` |
| `params` | JSON | 操作の引数 |
| `created_at` | timestamp | |

### 訂正が再計算で消えない仕組み

**問題**: Segment は再計算のたびに ID が変わる。`stay_id = 123 を「ホテル」にする` という訂正は、
再計算後に無意味になる。

**解決**: 訂正は Segment の ID ではなく **`anchor`（時刻範囲＋座標範囲）** に対して行う。

```json
{
  "time_range": ["2026-03-14T09:20:00Z", "2026-03-14T10:05:00Z"],
  "center": [35.0394, 135.7292],
  "radius_m": 150
}
```

再計算後、この anchor と重なる Segment に訂正を再適用する。
時刻と場所は再計算しても大きくは変わらないため、これで追随できる。

適用の流れ:

```
1. Observation から Segment を素朴に導出
2. Correction を anchor で照合し、重なる Segment に適用
3. 適用できなかった Correction は「孤児」として記録し、UI で提示する
```

**孤児の扱い**が重要。アルゴリズムを変えた結果として訂正が浮くことは必ず起きる。
黙って捨てるのではなく、ユーザーに「この訂正は行き場を失いました」と見せる。

---

## Annotation（注釈）

人が書いた文章。訂正とは別に管理する。

| フィールド | 型 | 説明 |
|---|---|---|
| `id` | ID | |
| `target_kind` | enum | `trip` / `day` / `stay` / `move` / `photo` |
| `anchor` | JSON | Correction と同じ方式 |
| `body` | text | Markdown |
| `created_at` / `updated_at` | timestamp | |

Correction と分けるのは、**性質が違うから**。訂正は「機械の間違いを直す」、
注釈は「機械が知り得ないことを足す」。前者は再計算で解決しうるが、後者は絶対に失えない。

---

## Asset（写真・動画アセット）

写真は Observation（`source = photo_exif`）として取り込むが、
**サムネイルと原本参照を持つため専用のエンティティを別に持つ**。

| フィールド | 型 | 説明 |
|---|---|---|
| `id` | ID | |
| `observation_id` | ID | 対応する Observation |
| `kind` | enum | `photo` / `video` / `live_photo` |
| `group_id` | ID, nullable | Live Photo / バーストのグループ |
| `is_group_primary` | bool | グループの代表か |
| `captured_at` | timestamp | UTC。TZ 解決後の値 |
| `captured_tz` | string, nullable | 解決した TZ |
| `tz_source` | enum | `exif_offset` / `from_coords` / `borrowed_from_gps` / `from_trip` / `unknown` |
| `lat` / `lon` | float, nullable | |
| `location_source` | enum | `exif` / `inferred_from_gps` / `user_set` / `unknown` |
| `orientation` | int | EXIF Orientation |
| `thumbnail_path` | string | **自前で保持するサムネイル** |
| `original_ref` | JSON | 原本への参照。`{"kind":"immich","asset_id":"..."}` / `{"kind":"file","path":"...","hash":"..."}` |
| `dedup_key` | string | 重複判定キー（撮影時刻＋機種＋画素数） |
| `camera_make` / `camera_model` | string, nullable | |

### 設計判断

**なぜ原本を持たず参照にするのか**

原本は重く、所有権が曖昧（Immich にある / NAS にある / iPhone にしかない）。
写真管理サーバの乗り換えは起きるし、そのたびに旅行記が壊れるのは許容できない。

**`original_ref` が解決できなくなっても、`thumbnail_path` とメタデータで旅行記は成立する。**
失われるのは拡大表示だけ。これが3層分離（メタ / サムネ / 原本）の実利である。

**なぜ `tz_source` と `location_source` を持つのか**

EXIF の `DateTimeOriginal` はタイムゾーンを持たず、位置情報を持たない写真も多い。
どちらも推定で埋めることになるため、**推定なのか確定なのかを区別できないと訂正の判断がつかない**。
Stay の `place_source`、Move の `mode_source` と同じ思想。

詳細は `docs/photos.md`。

---

## Place（場所マスタ）

複数ソースの場所表現を名寄せする。

| フィールド | 型 | 説明 |
|---|---|---|
| `id` | ID | |
| `name` | string | 表示名 |
| `lat` / `lon` | float | |
| `category` | string, nullable | `hotel` / `restaurant` / `station` / `attraction` / … |
| `external_refs` | JSON | `{"swarm_venue_id": "...", "osm_id": "node/123", "wikidata": "Q1234"}` |
| `merged_into` | ID, nullable | 名寄せ先。重複を検出したときに設定 |

### 名寄せが必要な理由

同じ「清水寺」に対して:

- Swarm の venue ID
- OSM の way ID
- 予約メールに書かれた文字列

がそれぞれ別に存在し、**座標も数百 m ずれる**。これを1つの Place に寄せないと、
「この場所に何回行ったか」が数えられない。

名寄せは自動（座標＋名称の近さ）で候補を出し、**確定は人が行う**。
自動で確定すると、間違えたときに戻せない。

---

## 検証シナリオ

このモデルで以下が表現できることを机上で確認する。

### 1. 海外旅行で日付変更線をまたぐ

- 全 Observation は UTC 保存、`observed_tz` に現地 TZ
- Trip の `home_tz` で「何日目」を計算、日の境界は現地 04:00
- ✅ 表現可能

### 2. 飛行機内で GPS が8時間途切れる

- 該当区間は Move（`is_gap = true`, `path = NULL`, `mode = flight`）
- 後からフライト情報を取り込んだら、新しい Observation として追加 → 再計算で `path` が埋まる
- ✅ 表現可能

### 3. 訂正がアルゴリズム変更後も残る

- Correction は Segment ID ではなく anchor（時刻範囲＋座標範囲）を持つ
- 再計算 → Segment 再生成 → Correction を anchor で再適用
- 適用できないものは孤児として提示
- ✅ 表現可能

### 4. Swarm チェックインと写真 EXIF が数百 m ずれる

- 両方 Observation として保持（どちらも消さない）
- Stay の `center_lat/lon` は導出時のルールで決める
- `place_source` に「どちらを採用したか」が残る
- ユーザーが不服なら Correction で上書き
- ✅ 表現可能

---

## 未決定事項

以下は実データを見ないと決められない。実装フェーズで検証する。

- 滞在判定の閾値（何分・何メートル以内に留まったら滞在か）
- 旅行判定の閾値（自宅から何 km 離れて何時間経過したら旅行か）
- Correction の anchor の照合ロジック（重なり率の閾値）
- Place 名寄せの候補提示ロジック
