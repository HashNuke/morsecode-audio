FROM golang:1.26.1-bookworm@sha256:ab3d6955bbc813a0f3fdf220c1d817dd89c0b3f283777db8ece4a32fe7858edd AS build
WORKDIR /src
COPY go.* ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/morsecode-audio ./cmd/morsecode-audio

FROM debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587
RUN apt-get update \
    && apt-get install --no-install-recommends -y \
       ffmpeg=7:5.1.9-0+deb12u1 libmp3lame0=3.100-6 libopus0=1.3.1-3 \
       ca-certificates=20250419~deb12u1 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/morsecode-audio /usr/local/bin/morsecode-audio
USER 65532:65532
ENV HOST=0.0.0.0
EXPOSE 8787
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/morsecode-audio"]
