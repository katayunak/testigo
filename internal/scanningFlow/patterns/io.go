package patterns

import "github.com/katayunak/testigo/internal/scanningFlow/flowEntity"

type Rule struct {
	Prefix string
	Kind   flowEntity.SeamKind
}

var IOPrefixes = []Rule{

	{Prefix: "database/sql", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/jackc/pgx", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/lib/pq", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/go-sql-driver/mysql", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/mattn/go-sqlite3", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/denisenkom/go-mssqldb", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/microsoft/go-mssqldb", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/jmoiron/sqlx", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/go-pg/pg", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/go-pg/migrations", Kind: flowEntity.SeamDB},
	{Prefix: "gorm.io/", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/jinzhu/gorm", Kind: flowEntity.SeamDB},
	{Prefix: "entgo.io/", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/uptrace/bun", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/volatiletech/sqlboiler", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/doug-martin/goqu", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/Masterminds/squirrel", Kind: flowEntity.SeamDB},

	{Prefix: "go.mongodb.org/", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/gocql/gocql", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/ClickHouse/clickhouse-go", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/aerospike/aerospike-client-go", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/elastic/go-elasticsearch", Kind: flowEntity.SeamDB},
	{Prefix: "go.etcd.io/etcd/client", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/hashicorp/consul/api", Kind: flowEntity.SeamDB},

	{Prefix: "go.elastic.co/apm/module/apmgopg", Kind: flowEntity.SeamDB},
	{Prefix: "go.elastic.co/apm/module/apmsql", Kind: flowEntity.SeamDB},
	{Prefix: "go.elastic.co/apm/module/apmmongo", Kind: flowEntity.SeamDB},
	{Prefix: "go.elastic.co/apm/module/apmredigo", Kind: flowEntity.SeamCache},
	{Prefix: "go.elastic.co/apm/module/apmgrpc", Kind: flowEntity.SeamHTTP},
	{Prefix: "go.elastic.co/apm/module/apmhttp", Kind: flowEntity.SeamHTTP},
	{Prefix: "go.opentelemetry.io/contrib/instrumentation/database", Kind: flowEntity.SeamDB},
	{Prefix: "go.opentelemetry.io/contrib/instrumentation/net/http", Kind: flowEntity.SeamHTTP},
	{Prefix: "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc", Kind: flowEntity.SeamHTTP},

	{Prefix: "google.golang.org/grpc", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/twitchtv/twirp", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/go-resty/resty", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/hashicorp/go-retryablehttp", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/imroc/req", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/parnurzeal/gorequest", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/valyala/fasthttp", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/aws/aws-sdk-go", Kind: flowEntity.SeamHTTP},
	{Prefix: "cloud.google.com/go/storage", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/minio/minio-go", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/hashicorp/vault/api", Kind: flowEntity.SeamHTTP},

	{Prefix: "github.com/stripe/stripe-go", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/adyen/adyen-go-api-library", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/plaid/plaid-go", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/braintree-go/braintree-go", Kind: flowEntity.SeamHTTP},

	{Prefix: "github.com/segmentio/kafka-go", Kind: flowEntity.SeamQueue},
	{Prefix: "github.com/IBM/sarama", Kind: flowEntity.SeamQueue},
	{Prefix: "github.com/Shopify/sarama", Kind: flowEntity.SeamQueue},
	{Prefix: "github.com/confluentinc/confluent-kafka-go", Kind: flowEntity.SeamQueue},
	{Prefix: "github.com/twmb/franz-go", Kind: flowEntity.SeamQueue},
	{Prefix: "github.com/nats-io/", Kind: flowEntity.SeamQueue},
	{Prefix: "github.com/rabbitmq/", Kind: flowEntity.SeamQueue},
	{Prefix: "github.com/streadway/amqp", Kind: flowEntity.SeamQueue},
	{Prefix: "github.com/apache/pulsar-client-go", Kind: flowEntity.SeamQueue},
	{Prefix: "github.com/hibiken/asynq", Kind: flowEntity.SeamQueue},
	{Prefix: "github.com/RichardKnop/machinery", Kind: flowEntity.SeamQueue},
	{Prefix: "github.com/ThreeDotsLabs/watermill", Kind: flowEntity.SeamQueue},
	{Prefix: "cloud.google.com/go/pubsub", Kind: flowEntity.SeamQueue},

	{Prefix: "github.com/redis/", Kind: flowEntity.SeamCache},
	{Prefix: "github.com/go-redis/", Kind: flowEntity.SeamCache},
	{Prefix: "github.com/gomodule/redigo", Kind: flowEntity.SeamCache},
	{Prefix: "github.com/bradfitz/gomemcache", Kind: flowEntity.SeamCache},

	{Prefix: "github.com/benbjohnson/clock", Kind: flowEntity.SeamClock},
	{Prefix: "github.com/jonboulle/clockwork", Kind: flowEntity.SeamClock},

	{Prefix: "github.com/google/uuid", Kind: flowEntity.SeamRandom},
	{Prefix: "github.com/gofrs/uuid", Kind: flowEntity.SeamRandom},
	{Prefix: "github.com/satori/go.uuid", Kind: flowEntity.SeamRandom},
	{Prefix: "github.com/oklog/ulid", Kind: flowEntity.SeamRandom},
	{Prefix: "github.com/rs/xid", Kind: flowEntity.SeamRandom},
	{Prefix: "github.com/segmentio/ksuid", Kind: flowEntity.SeamRandom},
	{Prefix: "github.com/bwmarrin/snowflake", Kind: flowEntity.SeamRandom},
	{Prefix: "github.com/sony/sonyflake", Kind: flowEntity.SeamRandom},
	{Prefix: "math/rand", Kind: flowEntity.SeamRandom},
	{Prefix: "crypto/rand", Kind: flowEntity.SeamRandom},
}

var NetHTTPClient = map[string]bool{
	"net/http.Get": true, "net/http.Post": true, "net/http.PostForm": true, "net/http.Head": true,
	"(*net/http.Client).Do": true, "(*net/http.Client).Get": true, "(*net/http.Client).Post": true,
	"(*net/http.Client).Head": true, "(*net/http.Client).PostForm": true,
	"(*net/http.Transport).RoundTrip": true,
}

var CursorNoise = map[string]bool{
	"Next": true, "Scan": true, "Err": true, "Close": true, "Columns": true,
	"ColumnTypes": true, "NextResultSet": true, "LastInsertId": true, "RowsAffected": true,
}

var TimeSeamFuncs = map[string]bool{
	"Now": true, "Since": true, "Until": true, "Sleep": true, "After": true, "Tick": true,
}
