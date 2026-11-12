FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/app ./cmd/app \
 && CGO_ENABLED=0 go build -o /out/thirdparty ./cmd/thirdparty

FROM alpine:3
COPY --from=build /out/ /usr/local/bin/
