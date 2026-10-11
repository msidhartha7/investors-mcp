// Package models holds Go types for the MongoDB collections the server reads.
package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// CanonicalInvestorsCollection is the collection CanonicalInvestor is stored in.
const CanonicalInvestorsCollection = "canonical_investors"

// CanonicalInvestor is one deduplicated investor in isync_new.canonical_investors.
//
// The type was derived from a full scan of the collection (224,162 docs, Oct 2026). Notes from that scan:
//   - Indexed: _id and canonicalId (canonicalId is not unique-constrained, but has no duplicates).
//   - details and investorType can be null. Null decodes to "".
//   - aliases, fieldProvenance, recentInvestments, sourceUrls are always empty, and lastVerifiedAt
//     and website are always null. Their element types are guesses or left raw until real data shows up.
//   - freshnessScore and qualityScore are always 0, reviewState is always "needs_review", and status is always "unknown".
//   - One doc (_id 69e5b8adc5394959e1ad885b, canonical_id "oleg-morajko") uses a different snake_case
//     shape and decodes to a mostly empty value. See IsLegacySnakeCase.
type CanonicalInvestor struct {
	ID          bson.ObjectID `bson:"_id" json:"id"`
	CanonicalID string        `bson:"canonicalId" json:"canonicalId"` // stable key, e.g. "01-ventures"
	Slug        string        `bson:"slug" json:"slug"`               // usually equals CanonicalID (differs for 41 names containing "_")
	Name        string        `bson:"name" json:"name"`
	Aliases     []string      `bson:"aliases" json:"aliases,omitempty"`
	Details     string        `bson:"details" json:"details,omitempty"` // free-text bio, up to ~16k chars

	InvestorType       InvestorType `bson:"investorType" json:"investorType,omitempty"`
	InvestorStageFocus []Stage      `bson:"investorStageFocus" json:"investorStageFocus"`
	IndustryFocus      []string     `bson:"industryFocus" json:"industryFocus"`   // ~450 distinct values, e.g. "SaaS", "Healthcare"
	GeographyFocus     []string     `bson:"geographyFocus" json:"geographyFocus"` // ~1,700 distinct values, free-form

	Website           *string         `bson:"website" json:"website,omitempty"`
	SourceURLs        []string        `bson:"sourceUrls" json:"sourceUrls,omitempty"`
	SourceInvestorIDs []int64         `bson:"sourceInvestorIds" json:"sourceInvestorIds"` // ids in the upstream source; one per doc except 2
	RecentInvestments []bson.RawValue `bson:"recentInvestments" json:"-"`
	FieldProvenance   []bson.RawValue `bson:"fieldProvenance" json:"-"`

	FreshnessScore int         `bson:"freshnessScore" json:"freshnessScore"`
	QualityScore   int         `bson:"qualityScore" json:"qualityScore"`
	ReviewState    ReviewState `bson:"reviewState" json:"reviewState"`
	Status         string      `bson:"status" json:"status"`
	LastVerifiedAt *time.Time  `bson:"lastVerifiedAt" json:"lastVerifiedAt,omitempty"`
}

// IsLegacySnakeCase reports whether the doc used the snake_case shape and so decoded without the canonical fields.
func (c *CanonicalInvestor) IsLegacySnakeCase() bool { return c.CanonicalID == "" }

// InvestorType is the investorType field. These are all values seen in the data.
type InvestorType string

const (
	InvestorTypeAngelInvestors            InvestorType = "Angel Investors"
	InvestorTypeVentureCapital            InvestorType = "Venture Capital"
	InvestorTypeInvestmentBanks           InvestorType = "Investment Banks & Service Providers"
	InvestorTypeLimitedPartners           InvestorType = "Limited Partners"
	InvestorTypePEBuyout                  InvestorType = "PE/Buyout"
	InvestorTypeRealEstatePE              InvestorType = "Real Estate Private Equities"
	InvestorTypeAcceleratorsIncubators    InvestorType = "Accelerators/Incubators"
	InvestorTypeInstitutionalInvestors    InvestorType = "Institutional Investors"
	InvestorTypeFamilyOffices             InvestorType = "Family Offices"
	InvestorTypeCorporateVC               InvestorType = "Corporate Venture Capital"
	InvestorTypeAngelGroups               InvestorType = "Angel Groups"
	InvestorTypeFoundations               InvestorType = "Foundations"
	InvestorTypeLenders                   InvestorType = "Lenders"
	InvestorTypeRegisteredInvestmentAdvis InvestorType = "Registered Investment Advisors"
	InvestorTypeEndowment                 InvestorType = "Endowment"
	InvestorTypeTrusts                    InvestorType = "Trusts"
	InvestorTypeFundOfFunds               InvestorType = "Fund of Funds"
)

// Stage is an element of investorStageFocus. These are all values seen in the data.
type Stage string

const (
	StagePreSeed           Stage = "Pre Seed"
	StageSeedRound         Stage = "Seed Round"
	StageEarlyStageVC      Stage = "Early stage VC"
	StageLaterStageVC      Stage = "Later stage VC"
	StagePEGrowthExpansion Stage = "PE Growth/Expansion"
	StageBuyout            Stage = "Buyout"
	StageRecapitalization  Stage = "Recapitalization"
	StageMergersAcqs       Stage = "Mergers and Acquisitions"
	StageDebt              Stage = "Debt"
	StageMezzanine         Stage = "Mezzanine"
	StageOther             Stage = "Other"
)

// ReviewState is the reviewState field. Only "needs_review" is seen so far.
type ReviewState string

const ReviewStateNeedsReview ReviewState = "needs_review"
