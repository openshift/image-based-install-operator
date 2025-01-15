FROM registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.22-openshift-4.17 as builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

# Copy the go source
COPY cmd/ cmd/
COPY api/ api/
COPY controllers/ controllers/
COPY internal/ internal/
COPY vendor/ vendor/

RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/manager/main.go
RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o server cmd/server/main.go

FROM registry.access.redhat.com/ubi9/ubi-minimal:9.4

#RUN microdnf install glibc-devel

ARG DATA_DIR=/data
RUN mkdir $DATA_DIR && chmod 775 $DATA_DIR

WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/server .
USER 65532:65532

ENTRYPOINT ["/manager"]
