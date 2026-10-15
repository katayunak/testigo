package scan

import (
	"strings"

	"github.com/katayunak/gofacts/fact"
)

// ioPrefix nominates a package as a boundary of some kind. These rows are the
// heuristic part of seam detection and the first thing to check against a new
// repository's go.mod: a missing row means the flow is reported as never
// touching that boundary, a confident wrong answer.
type ioPrefix struct {
	prefix string
	kind   fact.SeamKind
}

var ioPrefixes = []ioPrefix{
	{"database/sql", fact.SeamDB},
	{"github.com/jackc/pgx", fact.SeamDB},
	{"github.com/lib/pq", fact.SeamDB},
	{"github.com/go-sql-driver/mysql", fact.SeamDB},
	{"github.com/mattn/go-sqlite3", fact.SeamDB},
	{"modernc.org/sqlite", fact.SeamDB},
	{"github.com/denisenkom/go-mssqldb", fact.SeamDB},
	{"github.com/microsoft/go-mssqldb", fact.SeamDB},
	{"github.com/jmoiron/sqlx", fact.SeamDB},
	{"github.com/go-pg/pg", fact.SeamDB},
	{"github.com/go-pg/migrations", fact.SeamDB},
	{"gorm.io/", fact.SeamDB},
	{"github.com/jinzhu/gorm", fact.SeamDB},
	{"entgo.io/", fact.SeamDB},
	{"github.com/uptrace/bun", fact.SeamDB},
	{"github.com/volatiletech/sqlboiler", fact.SeamDB},
	{"github.com/doug-martin/goqu", fact.SeamDB},
	{"github.com/Masterminds/squirrel", fact.SeamDB},
	{"github.com/georgysavva/scany", fact.SeamDB},
	{"go.mongodb.org/", fact.SeamDB},
	{"github.com/gocql/gocql", fact.SeamDB},
	{"github.com/ClickHouse/clickhouse-go", fact.SeamDB},
	{"github.com/aerospike/aerospike-client-go", fact.SeamDB},
	{"github.com/elastic/go-elasticsearch", fact.SeamDB},
	{"go.etcd.io/etcd/client", fact.SeamDB},
	{"github.com/hashicorp/consul/api", fact.SeamDB},
	{"cloud.google.com/go/spanner", fact.SeamDB},
	{"github.com/aws/aws-sdk-go-v2/service/dynamodb", fact.SeamDB},

	{"go.elastic.co/apm/module/apmgopg", fact.SeamDB},
	{"go.elastic.co/apm/module/apmsql", fact.SeamDB},
	{"go.elastic.co/apm/module/apmmongo", fact.SeamDB},
	{"go.elastic.co/apm/module/apmredigo", fact.SeamCache},
	{"go.elastic.co/apm/module/apmgrpc", fact.SeamHTTP},
	{"go.elastic.co/apm/module/apmhttp", fact.SeamHTTP},
	{"go.opentelemetry.io/contrib/instrumentation/database", fact.SeamDB},
	{"go.opentelemetry.io/contrib/instrumentation/net/http", fact.SeamHTTP},
	{"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc", fact.SeamHTTP},

	{"google.golang.org/grpc", fact.SeamHTTP},
	{"connectrpc.com/connect", fact.SeamHTTP},
	{"github.com/twitchtv/twirp", fact.SeamHTTP},
	{"github.com/go-resty/resty", fact.SeamHTTP},
	{"github.com/hashicorp/go-retryablehttp", fact.SeamHTTP},
	{"github.com/imroc/req", fact.SeamHTTP},
	{"github.com/parnurzeal/gorequest", fact.SeamHTTP},
	{"github.com/valyala/fasthttp", fact.SeamHTTP},
	{"github.com/aws/aws-sdk-go", fact.SeamHTTP},
	{"cloud.google.com/go/storage", fact.SeamHTTP},
	{"github.com/minio/minio-go", fact.SeamHTTP},
	{"github.com/hashicorp/vault/api", fact.SeamHTTP},

	{"github.com/stripe/stripe-go", fact.SeamHTTP},
	{"github.com/adyen/adyen-go-api-library", fact.SeamHTTP},
	{"github.com/plaid/plaid-go", fact.SeamHTTP},
	{"github.com/braintree-go/braintree-go", fact.SeamHTTP},

	{"github.com/segmentio/kafka-go", fact.SeamQueue},
	{"github.com/IBM/sarama", fact.SeamQueue},
	{"github.com/Shopify/sarama", fact.SeamQueue},
	{"github.com/confluentinc/confluent-kafka-go", fact.SeamQueue},
	{"github.com/twmb/franz-go", fact.SeamQueue},
	{"github.com/nats-io/", fact.SeamQueue},
	{"github.com/rabbitmq/", fact.SeamQueue},
	{"github.com/streadway/amqp", fact.SeamQueue},
	{"github.com/apache/pulsar-client-go", fact.SeamQueue},
	{"github.com/hibiken/asynq", fact.SeamQueue},
	{"github.com/RichardKnop/machinery", fact.SeamQueue},
	{"github.com/ThreeDotsLabs/watermill", fact.SeamQueue},
	{"cloud.google.com/go/pubsub", fact.SeamQueue},
	{"github.com/aws/aws-sdk-go-v2/service/sqs", fact.SeamQueue},
	{"github.com/aws/aws-sdk-go-v2/service/sns", fact.SeamQueue},

	{"github.com/redis/", fact.SeamCache},
	{"github.com/go-redis/", fact.SeamCache},
	{"github.com/gomodule/redigo", fact.SeamCache},
	{"github.com/bradfitz/gomemcache", fact.SeamCache},

	{"github.com/benbjohnson/clock", fact.SeamClock},
	{"github.com/jonboulle/clockwork", fact.SeamClock},

	{"github.com/google/uuid", fact.SeamRandom},
	{"github.com/gofrs/uuid", fact.SeamRandom},
	{"github.com/satori/go.uuid", fact.SeamRandom},
	{"github.com/oklog/ulid", fact.SeamRandom},
	{"github.com/rs/xid", fact.SeamRandom},
	{"github.com/segmentio/ksuid", fact.SeamRandom},
	{"github.com/bwmarrin/snowflake", fact.SeamRandom},
	{"github.com/sony/sonyflake", fact.SeamRandom},
	{"math/rand", fact.SeamRandom},
	{"crypto/rand", fact.SeamRandom},
}

func kindsOfPackage(path string) kindSet {
	var k kindSet
	for _, e := range ioPrefixes {
		if strings.HasPrefix(path, e.prefix) {
			k |= fromSeamKind(e.kind)
		}
	}
	return k
}

func isDBPackage(path string) bool { return kindsOfPackage(path)&ksDB != 0 }

var netHTTPClient = map[string]bool{
	"net/http.Get": true, "net/http.Post": true, "net/http.PostForm": true, "net/http.Head": true,
	"(*net/http.Client).Do": true, "(*net/http.Client).Get": true, "(*net/http.Client).Post": true,
	"(*net/http.Client).Head": true, "(*net/http.Client).PostForm": true,
	"(*net/http.Transport).RoundTrip": true,
}

// cursorNoise are calls on a result set. The query is the seam; reporting every
// scan adds noise without adding a test.
var cursorNoise = map[string]bool{
	"Next": true, "Scan": true, "Err": true, "Close": true, "Columns": true,
	"ColumnTypes": true, "NextResultSet": true, "LastInsertId": true, "RowsAffected": true,
}

var timeSeamFuncs = map[string]bool{
	"Now": true, "Since": true, "Until": true, "Sleep": true, "After": true, "Tick": true,
}
