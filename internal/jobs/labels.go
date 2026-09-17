package jobs

// OperationIDLabel and ProjectLabel are the label keys turnip sets on
// every Runner Job it creates. They are exported because correlating a
// Job back to its Operation happens outside this package too, and a
// duplicated string literal is what makes a prefix change a
// multi-package edit rather than a one-line one.
//
// The prefix is a DNS subdomain turnip controls. Kubernetes does not
// verify prefix ownership, so any value works, but the convention is to
// name a domain you own rather than squat on one you don't.
const (
	OperationIDLabel = "turnip.ivan.vc/operation-id"
	ProjectLabel     = "turnip.ivan.vc/project"
)
