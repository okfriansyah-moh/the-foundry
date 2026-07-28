FROM golang:1.24-alpine
WORKDIR /app
COPY . .
RUN go test ./api/...
CMD ["sh", "-c", "echo template image ready"]
