package fairgate

import (
	"context"
	"iter"
	"net/http"

	"github.com/google/go-querystring/query"
)

// Contact is a member of the organisation with extended details.
type Contact struct {
	Basefields ContactBasefields `json:"basefields,omitzero"`
	// Status is the status of the contact.
	Status ContactStatus `json:"status,omitempty"`
	// Membership will not be present for federations.
	Membership *Membership `json:"membership,omitempty"`
	// FederationData will not be present for standard clubs.
	FederationData *Federation `json:"federation_data,omitempty"`
	// CorrAddress is the contact's correspondence address.
	CorrAddress Address `json:"corr_address,omitzero"`
	// InvoiceAddress is the contact's invoice address.
	InvoiceAddress Address `json:"invoice_address,omitzero"`
	// Communication is the contact's communication information.
	Communication Communication `json:"communication,omitzero"`
	// ClubAssignments are available for federation and sub federation only.
	ClubAssignments *ClubAssignments `json:"club_assignments,omitempty"`
	// SubfedAssignments are available for federation only.
	SubfedAssignments []SubFedAssignment `json:"subfed_assignments,omitempty"`
}

// ContactsList is a single page of contacts.
type ContactsList struct {
	Pagination
	Contacts []Contact `json:"contacts,omitempty"`
}

// ContactStatus defines the status of a contact.
type ContactStatus string

const (
	ContactStatusActive   ContactStatus = "active"
	ContactStatusArchived ContactStatus = "archived"
	ContactStatusFFM      ContactStatus = "ffm"
	ContactStatusMembers  ContactStatus = "members"
)

// Communication represents communication information.
type Communication struct {
	// PrimaryEmail is the primary email address of the contact.
	PrimaryEmail string `json:"primary_email,omitempty"`
	// Mobile is the mobile number of the contact.
	Mobile string `json:"mobile,omitempty"`
	// Handy2 is an additional handy number of the contact.
	Handy2 string `json:"handy2,omitempty"`
	// EmailParent1 is the email address of the first parent.
	EmailParent1 string `json:"email_parent_1,omitempty"`
	// EmailParent2 is the email address of the second parent.
	EmailParent2 string `json:"email_parent_2,omitempty"`
	// Website is the website URL of the contact.
	Website string `json:"website,omitempty"`
	// CorrespondenceLanguage is the language used for correspondence with the contact.
	CorrespondenceLanguage Language `json:"correspondence_language,omitempty"`
}

// ContactType defines the type of a contact.
type ContactType string

const (
	ContactTypeSinglePerson ContactType = "singleperson"
	ContactTypeCompany      ContactType = "company"
)

// Gender defines the gender of a contact.
type Gender string

const (
	GenderMale   Gender = "male"
	GenderFemale Gender = "female"
)

// Salutation defines the salutation of a contact.
type Salutation string

const (
	SalutationFormal   Salutation = "formal"
	SalutationInformal Salutation = "informal"
)

// Language defines the language of a contact.
type Language string

const (
	LanguageEN Language = "en"
	LanguageDE Language = "de"
	LanguageFR Language = "fr"
	LanguageIT Language = "it"
)

// ContactBasefields represents the base fields of a contact.
type ContactBasefields struct {
	// ContactID is the unique ID per contact of an organisation like a club or a federation.
	ContactID int `json:"contact_id,omitempty"`
	// FirstName is the first name of the contact.
	FirstName string `json:"first_name,omitempty"`
	// LastName is the last name of the contact.
	LastName string `json:"last_name,omitempty"`
	// CompanyName is the name of the company if the contact is a company.
	CompanyName string `json:"company_name,omitempty"`
	// ContactType specifies if the contact is a company or not.
	ContactType ContactType `json:"contact_type,omitempty"`
	// Salutation is the salutation of the contact.
	Salutation Salutation `json:"salutation,omitempty"`
	// CorrespondenceLanguage follows ISO 639-1 format.
	CorrespondenceLanguage Language `json:"correspondence_language,omitempty"`
	// Gender is the gender of the contact.
	Gender Gender `json:"gender,omitempty"`
	// LastUpdate is the date when the contact was last updated.
	LastUpdate Time `json:"last_update"`
}

// Membership represents membership information.
type Membership struct {
	// Membership is the membership type of the contact.
	Membership string `json:"membership,omitempty"`
	// FirstJoiningDate is the date and time when the contact first joined.
	FirstJoiningDate Time `json:"first_joining_date"`
}

// ExecutiveBoard represents executive board function assignments.
type ExecutiveBoard struct {
	// RoleID is the ID of the function.
	RoleID int `json:"role_id,omitempty"`
	// RoleName is the name of executive board function.
	RoleName string `json:"role_name,omitempty"`
}

// ClubAssignment represents club assignment information.
type ClubAssignment struct {
	// OrganizationID is the oid of the club.
	OrganizationID string `json:"organization_id,omitempty"`
	// Organization is the name of the club.
	Organization string `json:"organization,omitempty"`
	// Membership is the membership type of the contact.
	Membership *Membership `json:"membership,omitempty"`
	// ExecutiveBoard function assignments.
	ExecutiveBoard []ExecutiveBoard `json:"executive_board,omitempty"`
}

// SubFedAssignment represents sub-federation assignment information.
type SubFedAssignment struct {
	// OrganizationID is the oid of the club.
	OrganizationID string `json:"organization_id,omitempty"`
	// Organization is the name of the club.
	Organization string `json:"organization,omitempty"`
	// ExecutiveBoard function assignments.
	ExecutiveBoard []ExecutiveBoard `json:"executive_board,omitempty"`
}

// ClubAssignments represents primary and secondary club assignments.
type ClubAssignments struct {
	Primary   *ClubAssignment  `json:"primary,omitempty"`
	Secondary []ClubAssignment `json:"secondary,omitempty"`
}

// Federation represents federation-specific data.
type Federation struct {
	// FederationContactID is the contact ID within the federation.
	FederationContactID *int `json:"federation_contact_id,string,omitempty"`
	// FederationMembership is the fed membership of the contact.
	FederationMembership string `json:"federation_membership,omitempty"`
	// FederationFirstJoiningDate is the date and time when the contact first joined the federation.
	FederationFirstJoiningDate Time `json:"federation_first_joining_date"`
}

// Address represents address information.
type Address struct {
	// AliasName for the address.
	AliasName string `json:"alias_name,omitempty"`
	// Street of the address.
	Street string `json:"street,omitempty"`
	// City of the address.
	City string `json:"city,omitempty"`
	// State of the address.
	State string `json:"state,omitempty"`
	// PostalCode of the address.
	PostalCode string `json:"postale_code,omitempty"`
	// Country of the address.
	Country string `json:"country,omitempty"`
	// PostOfficeBox of the address.
	PostOfficeBox string `json:"post_office_box,omitempty"`
}

// Contact retrieves the extended details of a single contact.
func (c *Client) Contact(ctx context.Context, contactID int) (*Contact, error) {
	path := c.orgPath("/fsa/v2.0/contact/%s/contacts/%d/extended", contactID)

	req, err := c.newRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}

	contact, err := doJSON[Contact](c, req)
	if err != nil {
		return nil, err
	}

	return &contact, nil
}

// ContactsIter returns an iterator over all contacts, fetching pages as
// needed. Iteration stops after the first error.
func (c *Client) ContactsIter(ctx context.Context) iter.Seq2[Contact, error] {
	return iterate(ctx, func(ctx context.Context, p PageParams) ([]Contact, Pagination, error) {
		list, err := c.Contacts(ctx, p)
		if err != nil {
			return nil, Pagination{}, err
		}
		return list.Contacts, list.Pagination, nil
	})
}

// Contacts retrieves a single page of contacts.
func (c *Client) Contacts(ctx context.Context, params PageParams) (*ContactsList, error) {
	v, err := query.Values(params)
	if err != nil {
		return nil, err
	}

	req, err := c.newRequest(
		ctx,
		http.MethodGet,
		c.orgPath("/fsa/v2.0/contact/%s/contacts/extended"),
		v,
		nil,
	)
	if err != nil {
		return nil, err
	}

	list, err := doJSON[ContactsList](c, req)
	if err != nil {
		return nil, err
	}

	return &list, nil
}
