# Streaming Studio

Go バックエンドと Svelte フロントエンドで構成した、HLS 入力前提の Web 配信スタジオです。複数 HLS を画面上で合成し、画像・文字オーバーレイを重ねて、HLS 出力または YouTube Live 送出を行えます。配信の実体はサーバ側の `ffmpeg` プロセスなので、ブラウザを閉じても配信は継続します。

## できること

- 複数 HLS ソースのレイアウト編集
- 画像アップロードとオーバーレイ
- フォントアップロードと文字オーバーレイ
- GUI でのドラッグ移動・リサイズ
- Canvas / 各ソースの CSS 編集
- HLS 出力
- YouTube Live 向けプリセット付き FFmpeg 出力
- フロントエンドと同等機能の REST API

## 起動方法

```bash
docker compose up --build
```

`8080` はこの環境ですでに使用中だったため、公開ポートは `28080` にしています。起動後は `http://localhost:28080` を開いてください。

## 主要 API

- `GET /api/v1/state`
- `PUT /api/v1/state`
- `POST /api/v1/sources`
- `PUT /api/v1/sources/:id`
- `DELETE /api/v1/sources/:id`
- `POST /api/v1/assets/images`
- `POST /api/v1/assets/fonts`
- `POST /api/v1/stream/start`
- `POST /api/v1/stream/stop`
- `GET /live/live.m3u8`

## 配信モデル

- 入力ソースは `hls` / `image` / `text` の 3 種です。
- 映像合成はサーバ側で `ffmpeg` の `filter_complex` を組み立てて実行します。
- 音声は `audioSourceId` で選んだ HLS ソース、未指定なら最初の有効な HLS ソースを使います。
- HLS 出力時は `/data/output/hls` にローリング保存され、HTTP 配信されます。
- YouTube 出力時はローカル保存せず、RTMP に直接送出します。

## アップロード

- 画像は `POST /api/v1/assets/images`
- フォントは `POST /api/v1/assets/fonts`
- 保存先は Docker volume 上の `/data/assets/...`

## UI と CSS について

- Canvas とソースごとに `customCSS` / `styleCSS` を保持します。
- GUI プレビューでは CSS がそのまま反映されます。
- 実際の配信出力は `ffmpeg` でレンダリング可能な項目が中心です。位置・サイズ・不透明度・画像・文字・フォントは反映されますが、任意 CSS 全量を `ffmpeg` 側へ完全変換するものではありません。

## YouTube プリセット

初期状態で以下の YouTube 向け設定を持っています。

- `output.mode = youtube`
- `output.youTube.preset = youtube-default`
- `output.youTube.rtmpUrl = rtmp://a.rtmp.youtube.com/live2`

`youtube-default` では `-maxrate`, `-bufsize`, `-tune zerolatency` を自動付与します。追加オプションは GUI か API で 1 行 1 引数の配列として上書きできます。

## ログ

- サーバログ: `/data/logs/server.log`
- FFmpeg ログ: `/data/logs/ffmpeg.log`

## ローカル開発

バックエンドテスト:

```bash
GOCACHE=/tmp/go-build go test ./...
```

フロントエンド開発サーバ:

```bash
cd frontend
npm install
npm run dev
```

Vite 開発サーバは `/api`, `/uploads`, `/live` を `http://localhost:8080` にプロキシします。
