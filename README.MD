This project is a content of the [Build Your Own X](https://github.com/codecrafters-io/build-your-own-x).

Tutorial: https://kasvith.me/posts/lets-create-a-simple-lb-go/

up servers
```bash
  docker build -t server ./test
  docker compose up
```

up load balance
```bash
  go run main.go --backends http://localhost:3000,http://localhost:3001,http://localhost:3002 --port 8080
```