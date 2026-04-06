package dsl

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Parser parses DSL YAML and creates executable strategies
type Parser struct {
	validator  *Validator
	ExprEngine *ExpressionEngine
}

// NewParser creates a new parser
func NewParser() *Parser {
	return &Parser{
		validator:  NewValidator(),
		ExprEngine: NewExpressionEngine(),
	}
}

// Parse parses YAML content into a Strategy
func (p *Parser) Parse(yamlContent []byte) (*Strategy, error) {
	var strategy Strategy
	if err := yaml.Unmarshal(yamlContent, &strategy); err != nil {
		return nil, fmt.Errorf("yaml parse error: %w", err)
	}

	// Validate strategy
	if err := p.validator.Validate(&strategy); err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}

	return &strategy, nil
}

// ParseWithExpressions parses strategy and compiles expressions
func (p *Parser) ParseWithExpressions(yamlContent []byte) (*StrategyInstance, error) {
	strategy, err := p.Parse(yamlContent)
	if err != nil {
		return nil, err
	}

	instance := &StrategyInstance{
		ID:       generateID(),
		Strategy: strategy,
		Positions: make(map[string]*Position),
		Context:  &EvaluationContext{
			Indicators: make(map[string]float64),
			Candles:    make([]*Candle, 0, 100),
		},
		Metrics: &PerformanceMetrics{
			StrategyID: generateID(),
		},
		State: "active",
	}

	// Compile expressions
	if strategy.Long != nil {
		instance.LongEntry, err = p.ExprEngine.Compile(strategy.Long.Entry)
		if err != nil {
			return nil, fmt.Errorf("long entry compile error: %w", err)
		}
		instance.LongExit, err = p.ExprEngine.Compile(strategy.Long.Exit)
		if err != nil {
			return nil, fmt.Errorf("long exit compile error: %w", err)
		}
	}

	if strategy.Short != nil {
		instance.ShortEntry, err = p.ExprEngine.Compile(strategy.Short.Entry)
		if err != nil {
			return nil, fmt.Errorf("short entry compile error: %w", err)
		}
		instance.ShortExit, err = p.ExprEngine.Compile(strategy.Short.Exit)
		if err != nil {
			return nil, fmt.Errorf("short exit compile error: %w", err)
		}
	}

	return instance, nil
}

// Validator validates strategy definitions
type Validator struct{}

// NewValidator creates a new validator
func NewValidator() *Validator {
	return &Validator{}
}

// Validate validates a strategy
func (v *Validator) Validate(strategy *Strategy) error {
	if strategy.Name == "" {
		return fmt.Errorf("strategy name is required")
	}
	if strategy.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if strategy.Risk == nil {
		return fmt.Errorf("risk config is required")
	}
	if strategy.Risk.PositionSize <= 0 {
		return fmt.Errorf("position_size must be positive")
	}
	if strategy.Risk.MaxPositions < 1 {
		return fmt.Errorf("max_positions must be at least 1")
	}

	// Validate hedge mode has both long and short
	if strategy.Type == HedgeType {
		if strategy.Long == nil || strategy.Short == nil {
			return fmt.Errorf("hedge mode requires both long and short configs")
		}
	}

	// Validate stop loss ranges
	if strategy.Long != nil && strategy.Long.StopLoss <= 0 {
		return fmt.Errorf("long stop_loss must be positive")
	}
	if strategy.Short != nil && strategy.Short.StopLoss <= 0 {
		return fmt.Errorf("short stop_loss must be positive")
	}

	return nil
}

// ExpressionEngine evaluates DSL expressions
type ExpressionEngine struct {
	mu              sync.RWMutex
	functions       map[string]func([]float64) float64
	indicatorCache  map[string][]float64
}

// NewExpressionEngine creates a new expression engine
func NewExpressionEngine() *ExpressionEngine {
	engine := &ExpressionEngine{
		functions:      make(map[string]func([]float64) float64),
		indicatorCache: make(map[string][]float64),
	}
	engine.registerBuiltins()
	return engine
}

// registerBuiltins registers built-in functions
func (e *ExpressionEngine) registerBuiltins() {
	e.functions["max"] = maxVal
	e.functions["min"] = minVal
	e.functions["abs"] = absVal
	e.functions["sqrt"] = sqrtVal
}

// Expression represents a compiled expression
type Expression struct {
	AST *ExprNode
}

// ExprNode represents an expression node
type ExprNode struct {
	Type     string      // "literal", "variable", "binary", "function", "unary"
	Value    float64
	Variable string
	Operator string
	Left     *ExprNode
	Right    *ExprNode
	Args     []*ExprNode
}

// Compile compiles an expression string into an executable expression
func (e *ExpressionEngine) Compile(expr string) (*Expression, error) {
	ast, err := e.parse(expr)
	if err != nil {
		return nil, err
	}
	return &Expression{AST: ast}, nil
}

// parse parses an expression string into an AST
func (e *ExpressionEngine) parse(expr string) (*ExprNode, error) {
	// Remove whitespace
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("empty expression")
	}

	// Tokenize
	tokens, err := e.tokenize(expr)
	if err != nil {
		return nil, err
	}

	// Parse with operator precedence
	return e.parseExpression(tokens, 0)
}

// tokenize splits expression into tokens
func (e *ExpressionEngine) tokenize(expr string) ([]string, error) {
	var tokens []string

	// Regex for token matching
	numberRegex := regexp.MustCompile(`^\d+\.?\d*`)
	identifierRegex := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*`)
	operatorRegex := regexp.MustCompile(`^[+\-*/%<>=!&|]+`)
	whitespaceRegex := regexp.MustCompile(`^\s+`)

	i := 0
	for i < len(expr) {
		// Skip whitespace
		if whitespaceRegex.MatchString(expr[i:]) {
			i++
			continue
		}

		// Parentheses
		if expr[i] == '(' || expr[i] == ')' || expr[i] == ',' {
			tokens = append(tokens, string(expr[i]))
			i++
			continue
		}

		// Numbers
		if match := numberRegex.FindString(expr[i:]); match != "" {
			tokens = append(tokens, match)
			i += len(match)
			continue
		}

		// Identifiers (variables, functions)
		if match := identifierRegex.FindString(expr[i:]); match != "" {
			tokens = append(tokens, match)
			i += len(match)
			continue
		}

		// Operators
		if match := operatorRegex.FindString(expr[i:]); match != "" {
			tokens = append(tokens, match)
			i += len(match)
			continue
		}

		return nil, fmt.Errorf("unexpected character at position %d: %c", i, expr[i])
	}

	return tokens, nil
}

// parseExpression parses tokens with precedence
func (e *ExpressionEngine) parseExpression(tokens []string, precedence int) (*ExprNode, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty expression")
	}

	// Handle logical OR (lowest precedence)
	if precedence == 0 {
		node, err := e.parseBinaryOp(tokens, "||", 1)
		if err == nil {
			return node, nil
		}
		return e.parseExpression(tokens, 1)
	}

	// Handle logical AND
	if precedence == 1 {
		node, err := e.parseBinaryOp(tokens, "&&", 2)
		if err == nil {
			return node, nil
		}
		return e.parseExpression(tokens, 2)
	}

	// Handle comparison operators
	if precedence == 2 {
		for _, op := range []string{"<=", ">=", "==", "!=", "<", ">"} {
			node, err := e.parseBinaryOp(tokens, op, 3)
			if err == nil {
				return node, nil
			}
		}
		return e.parseExpression(tokens, 3)
	}

	// Handle additive operators
	if precedence == 3 {
		for _, op := range []string{"+", "-"} {
			node, err := e.parseBinaryOp(tokens, op, 4)
			if err == nil {
				return node, nil
			}
		}
		return e.parseExpression(tokens, 4)
	}

	// Handle multiplicative operators
	if precedence == 4 {
		for _, op := range []string{"*", "/", "%"} {
			node, err := e.parseBinaryOp(tokens, op, 5)
			if err == nil {
				return node, nil
			}
		}
		return e.parseExpression(tokens, 5)
	}

	// Handle unary operators and atoms (highest precedence)
	return e.parseAtom(tokens)
}

// parseBinaryOp parses binary operations
func (e *ExpressionEngine) parseBinaryOp(tokens []string, op string, nextPrecedence int) (*ExprNode, error) {
	// Find rightmost occurrence of operator at this precedence
	depth := 0
	for i := len(tokens) - 1; i >= 0; i-- {
		if tokens[i] == ")" {
			depth++
		} else if tokens[i] == "(" {
			depth--
		} else if depth == 0 && tokens[i] == op {
			left, err := e.parseExpression(tokens[:i], nextPrecedence)
			if err != nil {
				return nil, err
			}
			right, err := e.parseExpression(tokens[i+1:], nextPrecedence)
			if err != nil {
				return nil, err
			}
			return &ExprNode{
				Type:     "binary",
				Operator: op,
				Left:     left,
				Right:    right,
			}, nil
		}
	}

	// Operator not found at this level
	return nil, fmt.Errorf("operator not found")
}

// parseAtom parses atomic values (literals, variables, functions, parentheses)
func (e *ExpressionEngine) parseAtom(tokens []string) (*ExprNode, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty atom")
	}

	// Single token
	if len(tokens) == 1 {
		return e.parseToken(tokens[0])
	}

	// Parenthesized expression
	if tokens[0] == "(" && tokens[len(tokens)-1] == ")" {
		return e.parseExpression(tokens[1:len(tokens)-1], 0)
	}

	// Function call
	for i, token := range tokens {
		if token == "(" && i > 0 {
			funcName := tokens[i-1]
			if i+1 >= len(tokens) || tokens[len(tokens)-1] != ")" {
				return nil, fmt.Errorf("malformed function call")
			}

			// Parse arguments
			argTokens := e.splitArgs(tokens[i+1 : len(tokens)-1])
			var args []*ExprNode
			for _, arg := range argTokens {
				argNode, err := e.parseExpression(arg, 0)
				if err != nil {
					return nil, err
				}
				args = append(args, argNode)
			}

			return &ExprNode{
				Type: "function",
				Variable: funcName,
				Args: args,
			}, nil
		}
	}

	return nil, fmt.Errorf("invalid expression: %v", tokens)
}

// parseToken parses a single token
func (e *ExpressionEngine) parseToken(token string) (*ExprNode, error) {
	// Number
	if num, err := strconv.ParseFloat(token, 64); err == nil {
		return &ExprNode{
			Type:  "literal",
			Value: num,
		}, nil
	}

	// Variable or constant
	return &ExprNode{
		Type:     "variable",
		Variable: token,
	}, nil
}

// splitArgs splits comma-separated function arguments
func (e *ExpressionEngine) splitArgs(tokens []string) [][]string {
	var args [][]string
	var current []string
	depth := 0

	for _, token := range tokens {
		if token == "," && depth == 0 {
			args = append(args, current)
			current = nil
		} else {
			if token == "(" {
				depth++
			} else if token == ")" {
				depth--
			}
			current = append(current, token)
		}
	}

	if len(current) > 0 {
		args = append(args, current)
	}

	return args
}

// Evaluate evaluates an expression in a context
func (e *ExpressionEngine) Evaluate(expr *Expression, ctx *EvaluationContext) (float64, error) {
	return e.evalNode(expr.AST, ctx)
}

// Evaluate evaluates this expression in a context
func (expr *Expression) Evaluate(ctx *EvaluationContext) (float64, error) {
	engine := NewExpressionEngine()
	return engine.Evaluate(expr, ctx)
}

// evalNode evaluates an AST node
func (e *ExpressionEngine) evalNode(node *ExprNode, ctx *EvaluationContext) (float64, error) {
	switch node.Type {
	case "literal":
		return node.Value, nil

	case "variable":
		return e.getVariable(node.Variable, ctx)

	case "binary":
		left, err := e.evalNode(node.Left, ctx)
		if err != nil {
			return 0, err
		}
		right, err := e.evalNode(node.Right, ctx)
		if err != nil {
			return 0, err
		}
		return e.evalBinaryOp(node.Operator, left, right)

	case "function":
		args := make([]float64, len(node.Args))
		for i, arg := range node.Args {
			val, err := e.evalNode(arg, ctx)
			if err != nil {
				return 0, err
			}
			args[i] = val
		}
		return e.evalFunction(node.Variable, args, ctx)

	default:
		return 0, fmt.Errorf("unknown node type: %s", node.Type)
	}
}

// getVariable gets a variable value from context
func (e *ExpressionEngine) getVariable(name string, ctx *EvaluationContext) (float64, error) {
	ctx.Mu.RLock()
	defer ctx.Mu.RUnlock()

	switch name {
	case "price":
		return ctx.Price, nil
	case "volume":
		return ctx.Volume, nil
	case "funding_rate":
		return ctx.FundingRate, nil
	default:
		// Check indicators
		if val, ok := ctx.Indicators[name]; ok {
			return val, nil
		}
		return 0, fmt.Errorf("unknown variable: %s", name)
	}
}

// evalBinaryOp evaluates a binary operation
func (e *ExpressionEngine) evalBinaryOp(op string, left, right float64) (float64, error) {
	switch op {
	case "+":
		return left + right, nil
	case "-":
		return left - right, nil
	case "*":
		return left * right, nil
	case "/":
		if right == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return left / right, nil
	case "%":
		if right == 0 {
			return 0, fmt.Errorf("modulo by zero")
		}
		return float64(int(left) % int(right)), nil
	case "<":
		return boolToFloat(left < right), nil
	case ">":
		return boolToFloat(left > right), nil
	case "<=":
		return boolToFloat(left <= right), nil
	case ">=":
		return boolToFloat(left >= right), nil
	case "==":
		return boolToFloat(left == right), nil
	case "!=":
		return boolToFloat(left != right), nil
	case "&&":
		return boolToFloat(left > 0 && right > 0), nil
	case "||":
		return boolToFloat(left > 0 || right > 0), nil
	default:
		return 0, fmt.Errorf("unknown operator: %s", op)
	}
}

// evalFunction evaluates a function call
func (e *ExpressionEngine) evalFunction(name string, args []float64, ctx *EvaluationContext) (float64, error) {
	// Check built-in functions
	if fn, ok := e.functions[name]; ok {
		return fn(args), nil
	}

	// Check indicator functions with context
	switch name {
	case "ema", "sma", "rsi":
		if len(args) < 1 {
			return 0, fmt.Errorf("%s requires period argument", name)
		}
		period := int(args[0])
		key := fmt.Sprintf("%s_%d", name, period)
		if val, ok := ctx.Indicators[key]; ok {
			return val, nil
		}
		return 0, fmt.Errorf("indicator %s not calculated", name)
	}

	return 0, fmt.Errorf("unknown function: %s", name)
}

// boolToFloat converts boolean to float64
func boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}

// Built-in functions
func maxVal(args []float64) float64 {
	if len(args) == 0 {
		return 0
	}
	max := args[0]
	for _, v := range args[1:] {
		if v > max {
			max = v
		}
	}
	return max
}

func minVal(args []float64) float64 {
	if len(args) == 0 {
		return 0
	}
	min := args[0]
	for _, v := range args[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

func absVal(args []float64) float64 {
	if len(args) == 0 {
		return 0
	}
	if args[0] < 0 {
		return -args[0]
	}
	return args[0]
}

func sqrtVal(args []float64) float64 {
	if len(args) == 0 {
		return 0
	}
	return math.Sqrt(args[0])
}

// generateID generates a unique ID
func generateID() string {
	return fmt.Sprintf("id_%d", time.Now().UnixNano())
}
