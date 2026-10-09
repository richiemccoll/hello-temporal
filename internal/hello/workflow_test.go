package hello_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/example/hello-temporal/internal/hello"
)

type WorkflowSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
	act *hello.Activities
}

func TestWorkflowSuite(t *testing.T) { suite.Run(t, new(WorkflowSuite)) }

func (s *WorkflowSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
	s.act = &hello.Activities{}
	s.env.RegisterActivity(s.act)
}

func (s *WorkflowSuite) AfterTest(_, _ string) { s.env.AssertExpectations(s.T()) }

func (s *WorkflowSuite) TestSuccess() {
	cat := hello.Cat{ID: "c1", URL: "https://cdn/c1.png", Width: 1, Height: 2}
	s.env.OnActivity(s.act.FetchCat, mock.Anything).Return(cat, nil).Once()

	s.env.ExecuteWorkflow(hello.SayHello, hello.SayHelloInput{Name: "Ada"})

	s.True(s.env.IsWorkflowCompleted())
	s.Require().NoError(s.env.GetWorkflowError())
	var out hello.SayHelloOutput
	s.Require().NoError(s.env.GetWorkflowResult(&out))
	s.Equal("Hello, Ada! Here is a cat for you.", out.Greeting)
	s.Equal(cat, out.Cat)
}

func (s *WorkflowSuite) TestRetriesTransientActivityFailures() {
	cat := hello.Cat{ID: "after-retry"}
	s.env.OnActivity(s.act.FetchCat, mock.Anything).Return(hello.Cat{}, errors.New("boom")).Twice()
	s.env.OnActivity(s.act.FetchCat, mock.Anything).Return(cat, nil).Once()

	s.env.ExecuteWorkflow(hello.SayHello, hello.SayHelloInput{Name: "Ada"})

	s.Require().NoError(s.env.GetWorkflowError())
	var out hello.SayHelloOutput
	s.Require().NoError(s.env.GetWorkflowResult(&out))
	s.Equal("after-retry", out.Cat.ID)
}

func (s *WorkflowSuite) TestNonRetryableFailureFailsWorkflowImmediately() {
	s.env.OnActivity(s.act.FetchCat, mock.Anything).
		Return(hello.Cat{}, temporal.NewNonRetryableApplicationError("401", hello.ErrTypeCatAPIClient, nil)).
		Once()

	s.env.ExecuteWorkflow(hello.SayHello, hello.SayHelloInput{Name: "Ada"})

	err := s.env.GetWorkflowError()
	s.Require().Error(err)
	s.True(hasApplicationErrorType(err, hello.ErrTypeCatAPIClient), "error chain: %v", err)
}

// hasApplicationErrorType walks the whole error chain; the workflow wraps the activity
// failure, so the outermost ApplicationError is the wrapper, not the activity's.
func hasApplicationErrorType(err error, errType string) bool {
	for ; err != nil; err = errors.Unwrap(err) {
		var appErr *temporal.ApplicationError
		if errors.As(err, &appErr) && appErr.Type() == errType {
			return true
		}
	}
	return false
}

func (s *WorkflowSuite) TestGivesUpAfterMaxAttempts() {
	s.env.OnActivity(s.act.FetchCat, mock.Anything).
		Return(hello.Cat{}, errors.New("still down")).
		Times(int(hello.ActivityOptions.RetryPolicy.MaximumAttempts))

	s.env.ExecuteWorkflow(hello.SayHello, hello.SayHelloInput{Name: "Ada"})

	s.Require().ErrorContains(s.env.GetWorkflowError(), "still down")
}
