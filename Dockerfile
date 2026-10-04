FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/deepseek2api .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/deepseek2api /usr/local/bin/deepseek2api
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["deepseek2api"]
