package app

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

const defaultRubricThreshold = 0.7

func evaluatorForDataset(dataset domain.DatasetVersion) domain.EvaluatorVersion {
	evaluator := domain.EvaluatorVersion{}
	trimmed := strings.TrimSpace(string(dataset.Rubric))
	if trimmed != "" && trimmed != "{}" && trimmed != "null" {
		if strings.HasPrefix(trimmed, "[") {
			_ = json.Unmarshal(dataset.Rubric, &evaluator.Rubric)
		} else {
			_ = json.Unmarshal(dataset.Rubric, &evaluator)
			if len(evaluator.Rubric) == 0 {
				var document struct {
					Criteria []domain.RubricCriterion `json:"criteria"`
				}
				if json.Unmarshal(dataset.Rubric, &document) == nil {
					evaluator.Rubric = document.Criteria
				}
			}
		}
	}
	if evaluator.ID == "" {
		evaluator.ID = dataset.ID + "-evaluator"
	}
	if evaluator.Version == 0 {
		evaluator.Version = dataset.Version
	}
	if evaluator.Name == "" {
		evaluator.Name = dataset.Name + " 评分规则"
	}
	if evaluator.CreatedAt.IsZero() {
		evaluator.CreatedAt = dataset.CreatedAt
	}
	for index := range evaluator.Deterministic {
		criterion := &evaluator.Deterministic[index]
		if criterion.ID == "" {
			criterion.ID = fmt.Sprintf("deterministic-%d", index+1)
		}
		if criterion.Name == "" {
			criterion.Name = criterion.ID
		}
	}
	if len(evaluator.Rubric) == 0 && !datasetHasDeterministicContract(dataset) {
		evaluator.Rubric = []domain.RubricCriterion{{ID: "task-success", Name: "任务完成质量", Description: "输出满足用例目标和期望结果", Weight: 1, Threshold: defaultRubricThreshold, Critical: true}}
	}
	for index := range evaluator.Rubric {
		criterion := &evaluator.Rubric[index]
		if criterion.ID == "" {
			criterion.ID = fmt.Sprintf("rubric-%d", index+1)
		}
		if criterion.Name == "" {
			criterion.Name = criterion.ID
		}
		if criterion.Description == "" {
			criterion.Description = criterion.Name
		}
		if criterion.Weight <= 0 {
			criterion.Weight = 1
		}
		if criterion.Threshold <= 0 || criterion.Threshold > 1 {
			criterion.Threshold = defaultRubricThreshold
		}
	}
	if evaluator.PassPolicy.RubricThreshold <= 0 || evaluator.PassPolicy.RubricThreshold > 1 {
		evaluator.PassPolicy.RubricThreshold = defaultRubricThreshold
	}
	return evaluator
}

func datasetHasDeterministicContract(dataset domain.DatasetVersion) bool {
	if len(dataset.Cases) == 0 {
		return false
	}
	for _, item := range dataset.Cases {
		if len(expectedCriteria(item.Expected)) == 0 {
			return false
		}
	}
	return true
}

func (s *Service) freezeEvaluator(dataset domain.DatasetVersion, agent domain.AgentVersion) domain.EvaluatorVersion {
	evaluator := dataset.Evaluator
	if evaluator.ID == "" {
		evaluator = evaluatorForDataset(dataset)
	}
	if agent.RunnerType == "mock" {
		evaluator.Judge.Harness = "mock"
		evaluator.Judge.Model = ""
	} else {
		// Judge execution is platform-owned. Dataset versions freeze criteria and
		// thresholds, while the global system binding chooses the Judge model.
		evaluator.Judge.Harness = s.judgeHarness
		evaluator.Judge.Model = s.judgeModel
	}
	return evaluator
}

func deterministicResults(caseItem domain.Case, candidate CandidateResult, evaluator domain.EvaluatorVersion) ([]domain.CriterionResult, bool) {
	criteria := append([]domain.DeterministicCriterion(nil), evaluator.Deterministic...)
	criteria = append(criteria, expectedCriteria(caseItem.Expected)...)
	results := make([]domain.CriterionResult, 0, len(criteria))
	requiredPassed := true
	for _, criterion := range criteria {
		passed, reason := evaluateDeterministic(criterion, candidate.Output)
		passedValue, score := passed, 0.0
		if passed {
			score = 1
		}
		results = append(results, domain.CriterionResult{CriterionID: criterion.ID, Kind: "deterministic", Name: criterion.Name, Status: verdictStatus(passed), Passed: &passedValue, Score: &score, Reason: reason, Required: criterion.Required, Weight: 1})
		if criterion.Required && !passed {
			requiredPassed = false
		}
	}
	return results, requiredPassed
}

func expectedCriteria(expected map[string]any) []domain.DeterministicCriterion {
	result := []domain.DeterministicCriterion{}
	appendValue := func(id, name, operator string, value any) {
		result = append(result, domain.DeterministicCriterion{ID: id, Name: name, Operator: operator, Value: value, Required: true})
	}
	if value, ok := expected["exactOutput"]; ok {
		appendValue("expected-exact-output", "输出精确匹配", "equals", value)
	}
	if value, ok := expected["outputContains"]; ok {
		switch values := value.(type) {
		case []any:
			for index, item := range values {
				appendValue(fmt.Sprintf("expected-output-contains-%d", index+1), "输出包含必要内容", "contains", item)
			}
		default:
			appendValue("expected-output-contains", "输出包含必要内容", "contains", value)
		}
	}
	if value, ok := expected["outputRegex"]; ok {
		appendValue("expected-output-regex", "输出格式匹配", "regex", value)
	}
	if value, ok := expected["jsonValid"].(bool); ok && value {
		appendValue("expected-json-valid", "输出是有效 JSON", "json_valid", true)
	}
	return result
}

func evaluateDeterministic(criterion domain.DeterministicCriterion, output string) (bool, string) {
	value := fmt.Sprint(criterion.Value)
	switch criterion.Operator {
	case "equals":
		passed := strings.TrimSpace(output) == strings.TrimSpace(value)
		return passed, deterministicReason(passed, "输出与期望值一致", "输出与期望值不一致")
	case "contains":
		passed := strings.Contains(output, value)
		return passed, deterministicReason(passed, "输出包含必要内容", "输出缺少必要内容："+value)
	case "not_contains":
		passed := !strings.Contains(output, value)
		return passed, deterministicReason(passed, "输出未包含禁止内容", "输出包含禁止内容："+value)
	case "regex":
		expression, err := regexp.Compile(value)
		if err != nil {
			return false, "评分规则的正则表达式无效"
		}
		passed := expression.MatchString(output)
		return passed, deterministicReason(passed, "输出符合格式要求", "输出不符合格式要求")
	case "json_valid":
		var document any
		passed := json.Unmarshal([]byte(output), &document) == nil
		return passed, deterministicReason(passed, "输出是有效 JSON", "输出不是有效 JSON")
	default:
		return false, "不支持的确定性操作：" + criterion.Operator
	}
}

func deterministicReason(passed bool, success, failure string) string {
	if passed {
		return success
	}
	return failure
}

func verdictStatus(passed bool) string {
	if passed {
		return "pass"
	}
	return "fail"
}

func weightedRubric(results []domain.CriterionResult, threshold float64) (float64, bool) {
	weighted, totalWeight, criticalPassed := 0.0, 0.0, true
	for _, result := range results {
		if result.Kind != "rubric" || result.Score == nil {
			continue
		}
		weight := result.Weight
		if weight <= 0 {
			weight = 1
		}
		weighted += *result.Score * weight
		totalWeight += weight
		if result.Critical && (result.Passed == nil || !*result.Passed) {
			criticalPassed = false
		}
	}
	if totalWeight == 0 {
		return 1, criticalPassed
	}
	score := weighted / totalWeight
	return score, criticalPassed && score >= threshold
}

func roundCost(value float64) float64 { return math.Round(value*1_000_000) / 1_000_000 }
