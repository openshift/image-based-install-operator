FROM registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.22-openshift-4.17 as builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /opt/app-root/src

COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

# Copy the go source
COPY cmd/ cmd/
COPY api/ api/
COPY controllers/ controllers/
COPY internal/ internal/
COPY vendor/ vendor/

RUN GODEBUG=madvdontneed=1 GOGC=50 CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o build/manager cmd/manager/main.go
RUN GODEBUG=madvdontneed=1 GOGC=50 CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o build/server cmd/server/main.go

FROM registry.access.redhat.com/ubi9/ubi-minimal:9.4

ARG DATA_DIR=/data
RUN mkdir $DATA_DIR && chmod 775 $DATA_DIR

WORKDIR /
COPY --from=builder /opt/app-root/src/build/manager /usr/local/bin/
COPY --from=builder /opt/app-root/src/build/server /usr/local/bin/
USER 65532:65532

ENTRYPOINT ["/usr/local/bin/manager"]
