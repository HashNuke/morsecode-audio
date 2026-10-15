FROM golang:1.26.1-bookworm@sha256:ab3d6955bbc813a0f3fdf220c1d817dd89c0b3f283777db8ece4a32fe7858edd AS build
WORKDIR /src
COPY go.* ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/morsecode-audio ./cmd/morsecode-audio

FROM scratch
COPY --from=build /out/morsecode-audio /usr/local/bin/morsecode-audio
USER 65532:65532
ENV HOST=0.0.0.0
EXPOSE 8787
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/morsecode-audio"]
