ARG GO_IMAGE=golang:1.26-bookworm@sha256:a688600ca24f8a4d3ca77f95b0dd40704a9fc787c826660eb7ba0b641b8b175d
ARG RUNTIME_IMAGE=debian:bookworm-slim@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251
FROM ${GO_IMAGE} AS build
ENV GOPROXY=https://goproxy.cn,direct GOSUMDB=off
WORKDIR /src
COPY go.mod ./
COPY main.go ./
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/duanju .

FROM ${RUNTIME_IMAGE}
ENV TZ=Asia/Shanghai
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates ffmpeg tzdata tini curl && rm -rf /var/lib/apt/lists/* && groupadd --gid 1000 duanju && useradd --uid 1000 --gid duanju --no-create-home duanju && mkdir -p /data /downloads && chown duanju:duanju /data /downloads
COPY --from=build /out/duanju /usr/local/bin/duanju
USER 1000:1000
WORKDIR /data
VOLUME ["/data", "/downloads"]
EXPOSE 3333
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD curl --noproxy '*' --fail --silent --output /dev/null http://127.0.0.1:3333/ || exit 1
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/duanju"]
CMD ["-open=false", "-listen", "0.0.0.0:3333", "-data-dir", "/data", "-out", "/downloads", "-ffmpeg", "/usr/bin/ffmpeg"]
