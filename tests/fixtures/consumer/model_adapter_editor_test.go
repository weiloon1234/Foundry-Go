package consumer_test

import (
	"testing"

	"foundry.test/consumer/eventqueries"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// Keep the consumer-owned mutator signature visible after generated callers retire.
func TestConsumerMutatorMethod(t *testing.T) {
	member := mutatorqueries.Member{}
	stored, err := member.MutateEmail(" ADA@EXAMPLE.TEST ")
	if err != nil || stored != "ada@example.test" {
		t.Fatal("consumer mutator changed", err)
	}
}

// Identity adapters retain the same concrete model and key without generated methods.
func TestEventLinkIdentityAdapter(t *testing.T) {
	identity := orm.IdentityOf(eventqueries.Links.Table, eventqueries.Links.Key())
	reference := identity.ReferenceOf(eventqueries.EventLink{ID: 17})
	if reference.ModelName() != "event_links" || reference.Key() != int64(17) {
		t.Fatal("event reference changed")
	}
}
