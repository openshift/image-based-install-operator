FROM registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.24-openshift-4.21 as builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /opt/app-root/src

COPY go.mod go.mod
COPY go.sum go.sum

# Copy the go source
COPY cmd/ cmd/
COPY api/ api/
COPY controllers/ controllers/
COPY internal/ internal/
COPY vendor/ vendor/

RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o build/manager cmd/manager/main.go
RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o build/server cmd/server/main.go

FROM registry.ci.openshift.org/ocp/4.21:base-rhel9

ENV SUMMARY="The image-based-install-operator orchestrates image-based cluster installs from a central cluster using declarative APIs" \
    DESCRIPTION="The image-based-install operator creates the configuration ISO for image-based cluster installation and attaches that image to hosts using a BareMetalHost definition"

LABEL name="image-based-install-operator" \
      summary="${SUMMARY}" \
      description="${DESCRIPTION}" \
      com.redhat.component="image-based-install-operator" \
      io.k8s.display-name="Image Based Install Operator" \
      io.k8s.description="${DESCRIPTION}" \
      io.openshift.tags="install,cluster,provisioning"

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
