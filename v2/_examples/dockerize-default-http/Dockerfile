FROM golang:1.27.1-alpine AS build

ENV CGO_ENABLED=0
WORKDIR /src

# v2/, the v2 module directory, is the build context so the example's local replace works.
COPY . .
RUN go build -C _examples/dockerize-default-http -o /app/main .

FROM scratch AS run

COPY --from=build /app/main /app/main
COPY --from=build /src/_examples/asset /asset

EXPOSE 8000
ENTRYPOINT ["/app/main"]
