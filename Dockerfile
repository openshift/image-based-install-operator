FROM registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.24-openshift-4.20 as builder
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

RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o build/manager cmd/manager/main.go
RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o build/server cmd/server/main.go


FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:6fc28bcb6776e387d7a35a2056d9d2b985dc4e26031e98a2bd35a7137cd6fd71

ARG DATA_DIR=/data
RUN mkdir $DATA_DIR && chmod 775 $DATA_DIR

RUN microdnf install -y nmstate-libs nmstate && microdnf clean all

WORKDIR /
COPY --from=builder /opt/app-root/src/build/manager /usr/local/bin/
COPY --from=builder /opt/app-root/src/build/server /usr/local/bin/
USER 65532:65532
ENV GODEBUG=madvdontneed=1
ENV GOGC=50

ENTRYPOINT ["/usr/local/bin/manager"]
