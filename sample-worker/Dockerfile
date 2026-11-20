FROM golang:1.26.10 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /worker .

FROM build AS test
RUN go test ./...

FROM scratch AS runtime
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /worker /worker
USER 65532:65532
EXPOSE 9090
ENTRYPOINT ["/worker"]
