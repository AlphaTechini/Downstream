# Downstream memory service for Cloud Run. Built from the repository root:
#   docker build -f cloud/memory/Dockerfile .
# Plain Dockerfile syntax (no BuildKit-only flags) so Cloud Build's classic
# docker builder accepts it.

FROM golang:1.26-alpine3.24 AS build
WORKDIR /src
COPY go.mod ./
COPY engine ./engine
# Standard library only, so there is nothing to download first.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/downstream-memory ./engine/cmd/downstream-memory

# distroless/static carries CA certificates for the HTTPS calls to Firestore
# and GitLab, and runs as a non-root user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/downstream-memory /downstream-memory
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/downstream-memory"]
