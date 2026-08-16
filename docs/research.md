# 事前調査

調査日: 2026-08

## 1. 既存 OSS / サービス

### 位置情報タイムライン系（Google タイムライン代替）

このレイヤーは**飽和している**。同じものを作る意味はない。

| プロダクト | 実体 | 主な機能 | 備考 |
|---|---|---|---|
| [Dawarich](https://github.com/Freika/dawarich) | Ruby on Rails / AGPL-3.0 / 9,000★超 | 滞在・移動の自動検出、Immich・PhotoPrism 連携、独自 iOS/Android アプリ、家族位置共有、Google Takeout / OwnTracks / GPX / GeoJSON インポート | **この領域の事実上の標準**。Cloud 版（€59.99/年〜）もある。OwnTracks / Overland / GPSLogger / PhoneTrack / Home Assistant からの受信に対応 |
| [GeoPulse](https://github.com/tess1o/geopulse) | Java (Quarkus) + Vue / 2025〜 | 滞在・移動検出、Immich 連携、共有リンク（パスワード・失効付き）、招待・ロール・監査ログ | 省メモリ志向（100MB 未満を標榜）。OwnTracks(HTTP/MQTT) / Overland / GPSLogger / Home Assistant / Traccar / **Dawarich** / Colota から受信可能。**dawarich 形式を受ける実装の参考になる** |
| [Reitti](https://alternativeto.net/software/reitti/about) | Docker 配布 | 訪問・移動検出、**移動手段の推定**、頻繁に訪れる場所の学習、Immich 連携 | 移動手段推定は歩数データとの組み合わせで発展余地あり |
| [OwnTracks](https://owntracks.org/) | MQTT/HTTP ベース | 位置の記録と送信のみ | この分野の元祖。閲覧 UI を持たないため可視化は別途必要 |
| Traccar | Java | 車両トラッキング | 業務用途寄り |

### 旅行記・旅程系

| プロダクト | 実体 | 備考 |
|---|---|---|
| [AdventureLog](https://adventurelog.app/) | セルフホスト | 旅行記録、旅程プランナー、訪問国マップ、写真とメモ付きの場所ログ。**完全に手入力**で自動収集はしない |
| Polarsteps | 商用アプリ | 自動で軌跡を記録し旅行記化。コンセプトは最も近いがクローズド |
| Wanderlog / Journi | 商用アプリ | 旅程計画寄り |

### ライフログ統合系

| プロダクト | 実体 | 備考 |
|---|---|---|
| [Timelinize](https://github.com/timelinize/timelinize) | Go + SQLite / AGPL | **思想が最も近い**。写真・動画・メッセージ・位置・SNS を1本のタイムラインに統合。Google Takeout / Apple iCloud / Facebook / Instagram / X / Strava 等をインポート。重複排除、エンティティ（人・ペット・組織）のソース横断名寄せ、セマンティック検索、位置の推論。Mac/Windows/Linux/Docker |

**ただし**粒度が「人生全部」であり、旅行という区切りも読み物としての出力も持たない。

### 写真管理系（連携対象）

- **Immich** — セルフホスト写真管理のデファクト。上記3つの位置情報 OSS がすべて連携先に選んでいる
- **PhotoPrism** — 同種。Dawarich が連携対応

### 既存 OSS の写真の持ち方

**2つの流派に分かれ、「写真が主役に近いほどコピーする」という相関がある。**

| 流派 | プロダクト | 実装 |
|---|---|---|
| **参照モデル**（持たない） | Dawarich / GeoPulse / Reitti | Immich・PhotoPrism の API を叩いて位置メタを読むだけ。**サムネイルすら持たない**。Dawarich は「写真を保存もコピーもしない」と明言。外部が落ちると写真が丸ごと消える |
| **コピーモデル**（持つ） | AdventureLog / Timelinize | 原本を自前保管。Timelinize は「長期的な安定性と完全性のため」と理由を明示している |

本サービスは旅行記側（＝写真が主役に近い）ため、参照モデルでは弱く、
コピーモデルではスコープが破綻する。**中間を取る**。詳細は `docs/photos.md` §2。

## 2. データソース候補

### 当初想定していたもの

| ソース | 取得方法 | 得られる情報 | 注意点 |
|---|---|---|---|
| GPS（dawarich 形式 push） | HTTP POST 受信 | 連続的な軌跡、滞在の検出元 | 精度が場所により大きく変動。屋内・地下・トンネルで欠損 |
| Swarm チェックイン | Webhook | **確定した場所名**とカテゴリ、時刻 | Foursquare は 2024 年末に City Guide を終了。Swarm 自体は継続だが API の将来性は不透明。**取り込んだデータを自前で保持する設計にすべき** |
| Google Health 歩数 | Health Connect API | 歩数、（拡張すれば）心拍・睡眠・ワークアウト | 移動手段の推定材料になる。Android のみ。iOS は HealthKit が対応物 |
| KML | ファイルアップロード | 軌跡、任意の地点情報 | 出所が様々（Google Earth、各種アプリ）。スキーマの揺れが大きい |
| 写真（JPG / HEIC） | ファイル or 外部 API | EXIF の撮影時刻・GPS・向き | **HEIC の EXIF は ISO BMFF ボックス内**にあり JPEG 用パーサでは読めない。**EXIF の撮影時刻にタイムゾーンが無い**（`OffsetTimeOriginal` は機種依存）。位置情報が無い写真も多い（設定オフ、スクショ、受信画像）。詳細と対処は `docs/photos.md` |

### 追加候補 — A. 旅行の骨格を確定させる（最優先）

自動検出の精度を最も大きく引き上げるのはこの群。**「いつからいつまでが1つの旅行か」が確定する。**

| ソース | 取得方法 | 得られる情報 | 実装難度 |
|---|---|---|---|
| **予約確認メール** | Gmail API / IMAP + パーサ | 航空券、ホテル、レンタカー、新幹線、ツアー。**旅行の境界と宿泊地が確定** | 中〜高。メールのフォーマットが業者ごとに違うため、パーサの保守コストが継続的にかかる。TripIt が長年やっていること |
| **カレンダー** | Google Calendar API / CalDAV | 予定名、期間、同行者。予定名がそのままイベント名になる | 低 |
| **フライト情報** | フライト番号 → 経路。AeroDataBox / [OpenSky Network](https://opensky-network.org/) | 出発・到着空港と時刻、実際の飛行経路 | 中。OpenSky の履歴 API は認証・レート制限が厳しめ。**機内で GPS が切れる欠損区間を埋められる**のが最大の価値 |

### 追加候補 — B. 日本固有（差別化ポイント）

| ソース | 取得方法 | 得られる情報 | 注意点 |
|---|---|---|---|
| **交通系IC（Suica / PASMO）** | iOS: CoreNFC / Android: NFC で FeliCa 読み取り | **乗降駅と時刻**。地下鉄など GPS が途切れる区間を埋められる | **カード内に直近20件程度しか残らない**。「旅行から帰ったらかざす」運用が前提。駅コードは**サイバネコード**から駅名への変換テーブルが必要。[実装解説](https://qiita.com/m__ike_/items/7dc3e643396cf3381167)、[Suica Reader](https://play.google.com/store/apps/details?id=yanzm.products.suicareader)、Sony の SFCard Viewer 2 が先行例 |
| **ETC 利用照会サービス** | Web から CSV ダウンロード | 高速道路の IC 通過時刻と料金 | 手動ダウンロードが必要。車移動の区間が確定する |
| **EX 予約 / えきねっと** | 確認メール or スクレイピング | 新幹線の乗車区間・列車名・座席 | メールパースが現実的 |
| **GTFS / GTFS-RT** | 各事業者の公開データ | ダイヤ。「この時刻にこの区間 → ◯◯線△△行き」の推定 | データ整備状況が事業者により大きく異なる |

### 追加候補 — C. 体験の中身を埋める

| ソース | 取得方法 | 得られる情報 |
|---|---|---|
| 支出データ | Money Forward / Zaim API、カード明細 CSV、レシート OCR | **どこで何にいくら使ったか**。旅行費用の自動集計。為替 API と組めば現地通貨→円換算 |
| Strava / Garmin Connect | API | GPX 付きアクティビティ。ハイキング・サイクリングの詳細軌跡 |
| Untappd | API | 飲んだビールと場所。Swarm と相性が良い |
| YAMAP / ヤマレコ | — | 登山記録 |
| SNS 投稿 | Mastodon / Bluesky / X API、ブログ RSS | **その場で書いた文章**。そのまま旅行記の本文になる |
| 音楽再生履歴 | Last.fm / Spotify recently played | 移動中に聴いていた曲 |
| 動画の位置メタ | MP4 メタデータ、GoPro GPMF、ドラレコ NMEA | 動画の撮影地点と軌跡 |

### 追加候補 — D. 手間ゼロで後付けできる文脈

**ユーザーの操作を一切必要とせず**、座標と時刻さえあれば後から自動で足せる。費用対効果が非常に高い。

| ソース | 取得方法 | 得られる情報 |
|---|---|---|
| **過去天気** | [Open-Meteo Historical Weather API](https://open-meteo.com/) — **無料・APIキー不要** | 気温、降水、天候。「雨の中を歩いた2時間」が復元できる |
| **逆ジオコーディング** | Nominatim / Overpass API（OSM） | 座標 → POI 名、施設カテゴリ、市区町村、国 |
| **標高** | SRTM / Open-Elevation | 獲得標高、峠越え、山行プロファイル |
| **タイムゾーン** | 座標 → TZ 解決ライブラリ | **必須**。後述 |
| Wikipedia / Wikidata | API / SPARQL | 訪れた場所の説明文 |

### 使えない・注意が必要なもの

| ソース | 状況 |
|---|---|
| **Google Photos API** | 2025/3/31 に `photoslibrary.readonly` / `.sharing` / `photoslibrary` の各スコープが**削除**された。残るのは `photoslibrary.readonly.appcreateddata` のみで、**自アプリがアップロードしたものしか読めない**。代替の **Picker API** は「ユーザーが都度選ぶ」前提のため継続同期には使えない。**Takeout のワンショットか Immich 経由が現実的**。詳細は `docs/photos.md` |
| Foursquare / Swarm | Foursquare は 2024 年末に City Guide 終了。Swarm は継続中だが API の将来性は不透明 |
| OpenSky Network の履歴 API | 認証・レート制限が厳しい |

## 3. 調査から得た設計上の示唆

### タイムゾーンは最初から設計に入れる

海外旅行のタイムライン破綻の最大要因。以下がすべて絡む。

- 観測の記録時刻はどのタイムゾーンか（デバイスのローカル時刻 or UTC）
- 「その日」の境界はどこか（日付変更線をまたぐ旅行で「3日目」は何時から何時か）
- 表示は現地時間か自国時間か

**すべての観測を UTC で保存し、位置から導出した TZ を併記する**のが定石。後付けは困難なので最初から入れる。

### GPS の欠損は例外ではなく常態

- 飛行機内で8時間
- 地下鉄で30分
- バッテリー切れで半日
- 屋内で数時間、精度が数百 m に劣化

**欠損を「埋めるべき穴」として扱うのではなく、他ソース（フライト情報、IC カード、写真）で
補完可能な区間として明示的にモデル化する**必要がある。

### ソースごとに信頼度が違う

同じ「ここにいた」でも:

- Swarm チェックイン → 場所名が**確実**、時刻も確実
- 写真の EXIF → 位置は正確だが、**その場所の名前は不明**
- GPS の点列 → 位置は不正確、名前も不明

そして**同じ訪問に対して Swarm チェックインと写真の EXIF が数百 m ずれる**ことが普通に起きる。
どちらを採用するかのルールと、**採用理由をデータとして残す**仕組みが要る。

## 参考リンク

- [Dawarich](https://github.com/Freika/dawarich) / [公式サイト](https://dawarich.app/)
- [GeoPulse](https://github.com/tess1o/geopulse)
- [Timelinize](https://github.com/timelinize/timelinize) / [公式サイト](https://timelinize.com/)
- [AdventureLog](https://adventurelog.app/)
- [OwnTracks](https://owntracks.org/)
- [Open-Meteo](https://open-meteo.com/)
- [iOSでSuicaの履歴を読み取る](https://qiita.com/m__ike_/items/7dc3e643396cf3381167)
