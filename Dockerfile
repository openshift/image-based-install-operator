FROM registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.25-openshift-4.22 as builder
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


FROM registry.ci.openshift.org/ocp/4.21:base-rhel9

ARG DATA_DIR=/data
RUN mkdir $DATA_DIR && chmod 775 $DATA_DIR

RUN dnf install -y nmstate-libs-2.2.33-1.el9_4.x86_64 nmstate-2.2.33-1.el9_4.x86_64 && dnf clean all && rm -rf /var/cache/dnf/*

WORKDIR /
COPY --from=builder /opt/app-root/src/build/manager /usr/local/bin/
COPY --from=builder /opt/app-root/src/build/server /usr/local/bin/
USER 65532:65532
ENV GODEBUG=madvdontneed=1
ENV GOGC=50

ENTRYPOINT ["/usr/local/bin/manager"]
