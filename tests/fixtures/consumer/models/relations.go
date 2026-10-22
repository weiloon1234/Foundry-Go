package models

import "github.com/weiloon1234/Foundry-Go/database/query"

// defineUserRelations links Go field descriptors. Key/target compatibility is checked
// by the Go compiler; SQL column names and slot setters stay generator-owned.
func defineUserRelations() UserRelationSet {
	return UserRelationSet{
		Introducer:   query.BelongsTo(UserFields().IntroducerID, UserFields().ID),
		Referrals:    query.HasMany(UserFields().ID, UserFields().IntroducerID),
		Orders:       query.HasMany(UserFields().ID, OrderFields().BuyerID),
		SingleOrder:  query.HasOne(UserFields().ID, OrderFields().BuyerID),
		Groups:       query.ManyToMany(UserFields().ID, MembershipFields().UserID, MembershipFields().GroupCode, GroupFields().Code),
		Friends:      query.ManyToMany(UserFields().ID, FriendshipFields().FromID, FriendshipFields().ToID, UserFields().ID),
		Measurements: query.HasMany(UserFields().ID, MeasurementFields().UserID),
	}
}

func defineCountryRelations() CountryRelationSet {
	return CountryRelationSet{Locations: query.HasMany(CountryFields().Code, LocationFields().CountryCode)}
}
func defineLocationRelations() LocationRelationSet {
	return LocationRelationSet{Country: query.BelongsTo(LocationFields().CountryCode, CountryFields().Code)}
}
func defineOrderRelations() OrderRelationSet {
	return OrderRelationSet{Buyer: query.BelongsTo(OrderFields().BuyerID, UserFields().ID)}
}
