FROM golang:1.27.1 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /module .

FROM scratch
COPY --from=build /module /module
ENTRYPOINT ["/module"]
