FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /forge ./cmd/forge

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /forge /forge
EXPOSE 8080 9090
ENTRYPOINT ["/forge"]
CMD ["api"]
