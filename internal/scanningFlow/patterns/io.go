package patterns

import "github.com/katayunak/testigo/internal/scanningFlow/flowEntity"

// Rule maps a package path prefix to the kind of boundary it represents.
type Rule struct {
	Prefix string
	Kind   flowEntity.SeamKind
}

// ioPrefixes maps package path prefixes to the kind of boundary they represent.
//
// This is a lookup table on purpose. It is the piece most likely to need
// extending for a given team's stack, and a table is easier to review and
// extend than a chain of conditionals buried in the analysis.
//
// A prefix missing from this table is not a small problem. If a repository uses
// go-pg and go-pg is absent here, testigo reports that the payment flow never
// touches a database, which is worse than reporting nothing: it is a confident
// wrong answer. When adding a repository to testigo, check its direct
// dependencies against this table first.
//
// Every entry matches by prefix, and matches are OR'd together, so a broad
// prefix and a narrow one can both apply without conflict.
var IOPrefixes = []Rule{
	// SQL databases and the ORMs and query builders that wrap them.
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

	// Non-SQL stores. Coordination services belong here too: a distributed lock
	// held in etcd or consul is as much a correctness dependency as a row lock,
	// and idempotency is often built on one.
	{Prefix: "go.mongodb.org/", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/gocql/gocql", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/ClickHouse/clickhouse-go", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/aerospike/aerospike-client-go", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/elastic/go-elasticsearch", Kind: flowEntity.SeamDB},
	{Prefix: "go.etcd.io/etcd/client", Kind: flowEntity.SeamDB},
	{Prefix: "github.com/hashicorp/consul/api", Kind: flowEntity.SeamDB},

	// Instrumentation wrappers delegate to the driver underneath, so a call that
	// looks like tracing is a real database or network round trip. Missing these
	// hides every seam in a service that instruments its I/O, which is to say
	// every production service.
	{Prefix: "go.elastic.co/apm/module/apmgopg", Kind: flowEntity.SeamDB},
	{Prefix: "go.elastic.co/apm/module/apmsql", Kind: flowEntity.SeamDB},
	{Prefix: "go.elastic.co/apm/module/apmmongo", Kind: flowEntity.SeamDB},
	{Prefix: "go.elastic.co/apm/module/apmredigo", Kind: flowEntity.SeamCache},
	{Prefix: "go.elastic.co/apm/module/apmgrpc", Kind: flowEntity.SeamHTTP},
	{Prefix: "go.elastic.co/apm/module/apmhttp", Kind: flowEntity.SeamHTTP},
	{Prefix: "go.opentelemetry.io/contrib/instrumentation/database", Kind: flowEntity.SeamDB},
	{Prefix: "go.opentelemetry.io/contrib/instrumentation/net/http", Kind: flowEntity.SeamHTTP},
	{Prefix: "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc", Kind: flowEntity.SeamHTTP},

	// Outbound RPC and HTTP.
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

	// Payment provider SDKs. A call into one of these IS the money leaving, so
	// it deserves the same attention as a raw HTTP client and is easier to spot
	// by name than by tracing through a wrapper.
	{Prefix: "github.com/stripe/stripe-go", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/adyen/adyen-go-api-library", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/plaid/plaid-go", Kind: flowEntity.SeamHTTP},
	{Prefix: "github.com/braintree-go/braintree-go", Kind: flowEntity.SeamHTTP},

	// Brokers and job queues.
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

	// Caches. In-process caches such as bigcache are deliberately absent: they
	// never leave the process, so there is no failure to inject.
	{Prefix: "github.com/redis/", Kind: flowEntity.SeamCache},
	{Prefix: "github.com/go-redis/", Kind: flowEntity.SeamCache},
	{Prefix: "github.com/gomodule/redigo", Kind: flowEntity.SeamCache},
	{Prefix: "github.com/bradfitz/gomemcache", Kind: flowEntity.SeamCache},

	// Injectable clock libraries. Their presence is good news: it means expiry
	// and backoff logic can already be tested without waiting in real time.
	{Prefix: "github.com/benbjohnson/clock", Kind: flowEntity.SeamClock},
	{Prefix: "github.com/jonboulle/clockwork", Kind: flowEntity.SeamClock},

	// Identity and randomness. ID generation belongs here because idempotency
	// keys are made of it, and a retry that generates a fresh key is not
	// idempotent no matter what the handler does with it afterwards.
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

// netHTTPClient lists the outbound calls in net/http by FULL name.
//
// Matching on the bare method name was a bug: net/http.Header.Get is not an
// outbound request, and neither is net/http.Error, but both are called "Get"
// and "Error" on a package path of net/http. A payment service is full of
// server-side net/http types, and misclassifying them buries the two provider
// calls that actually matter under fifty that do not.
var NetHTTPClient = map[string]bool{
	"net/http.Get": true, "net/http.Post": true, "net/http.PostForm": true, "net/http.Head": true,
	"(*net/http.Client).Do": true, "(*net/http.Client).Get": true, "(*net/http.Client).Post": true,
	"(*net/http.Client).Head": true, "(*net/http.Client).PostForm": true,
	"(*net/http.Transport).RoundTrip": true,
}

// cursorNoise are result-set mechanics rather than boundaries worth injecting
// a fault at. They do perform I/O in some drivers, but a report that lists
// Rows.Next alongside the provider authorization call has buried the signal.
// The query is the seam; iterating its result is not.
var CursorNoise = map[string]bool{
	"Next": true, "Scan": true, "Err": true, "Close": true, "Columns": true,
	"ColumnTypes": true, "NextResultSet": true, "LastInsertId": true, "RowsAffected": true,
}

// timeSeamFuncs are the reads that make behaviour depend on wall-clock time.
// Expiries, idempotency-key windows and retry backoff all hinge on these, and
// a test cannot exercise them without an injectable clock.
var TimeSeamFuncs = map[string]bool{
	"Now": true, "Since": true, "Until": true, "Sleep": true, "After": true, "Tick": true,
}
