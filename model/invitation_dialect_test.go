package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DryRun with automatic ping and MySQL version probing disabled never connects
// to an external database. SQLite's actual transactional behavior is tested by
// the purchase tests; here we check the two production SQL dialects separately.
func TestInvitationSQLDialectLocksAndLargeTotals(t *testing.T) {
	for name, dialector := range map[string]gorm.Dialector{
		"mysql":    mysql.New(mysql.Config{DSN: "synthetic@tcp(127.0.0.1:1)/synthetic", SkipInitializeWithVersion: true}),
		"postgres": postgres.Open("host=127.0.0.1 port=1 user=synthetic dbname=synthetic sslmode=disable"),
	} {
		t.Run(name, func(t *testing.T) {
			db, err := gorm.Open(dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			require.NoError(t, err)
			conn, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			query := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", "synthetic-order").First(&TopUp{})
			require.NoError(t, query.Error)
			require.Contains(t, query.Statement.SQL.String(), "FOR UPDATE")
			statement := &gorm.Statement{DB: db}
			require.NoError(t, statement.Parse(&User{}))
			for _, column := range []string{"aff_quota", "aff_history"} {
				field := statement.Schema.LookUpField(column)
				require.NotNil(t, field)
				require.True(t, strings.HasPrefix(strings.ToLower(db.Migrator().FullDataTypeOf(field).SQL), "bigint"))
			}
		})
	}
}
