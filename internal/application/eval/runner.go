// Package eval implements the Level 2 Cognitive Evaluation harness (`romero eval`)
// measuring NDCG@5, multi-hop recall, obsolete suppression, and Zero-Trust leakage.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	contextbuilder "github.com/romero-project/romero/internal/application/context"
	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/governor"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/cachel1"
	"github.com/romero-project/romero/internal/infrastructure/neural"
)

// Report summarizes Level 2 Cognitive Evaluation metrics over a golden dataset.
type Report struct {
	DatasetPath             string  `json:"dataset_path"`
	CasesEvaluated          int     `json:"cases_evaluated"`
	NDCGAt5                 float64 `json:"ndcg_at_5"`
	MultiHopRecall          float64 `json:"multi_hop_recall"`
	ObsoleteSuppressionRate float64 `json:"obsolete_suppression_rate"`
	ZeroTrustLeakageRate    float64 `json:"zero_trust_leakage_rate"`
	ContradictionCatchRate  float64 `json:"contradiction_catch_rate"`
	REMDistillationRatio    float64 `json:"rem_distillation_ratio"`
	Pass                    bool    `json:"pass"`
}

type goldenDataset struct {
	Memories    []goldenMemory   `json:"memories"`
	Relations   []goldenRelation `json:"relations"`
	Queries     []goldenQuery    `json:"queries"`
	NLIPairs    []goldenNLIPair  `json:"nli_pairs,omitempty"`
	REMEpisodes []goldenMemory   `json:"rem_episodes,omitempty"`
}

type goldenNLIPair struct {
	Name           string `json:"name"`
	Premise        string `json:"premise"`
	Hypothesis     string `json:"hypothesis"`
	ExpectedAction string `json:"expected_action"`
}

type goldenMemory struct {
	ID              uuid.UUID              `json:"id"`
	ScopeType       identity.ScopeType     `json:"scope_type"`
	Owner           string                 `json:"owner"`
	Type            memory.MemoryType      `json:"type"`
	Status          memory.MemoryStatus    `json:"status"`
	EpistemicStatus memory.EpistemicStatus `json:"epistemic_status"`
	Confidence      float32                `json:"confidence"`
	Importance      float32                `json:"importance"`
	UtilityScore    float32                `json:"utility_score"`
	TopicKey        string                 `json:"topic_key"`
	Summary         string                 `json:"summary"`
	Content         string                 `json:"content"`
	Tags            []string               `json:"tags"`
	DenseVec        []float32              `json:"dense_vec"`
}

type goldenRelation struct {
	FromID       uuid.UUID           `json:"from_id"`
	ToID         uuid.UUID           `json:"to_id"`
	RelationType memory.RelationType `json:"relation_type"`
	Weight       float32             `json:"weight"`
}

type goldenQuery struct {
	Name                string      `json:"name"`
	Query               string      `json:"query"`
	QueryVec            []float32   `json:"query_vec"`
	ExpectedTopIDs      []uuid.UUID `json:"expected_top_ids"`
	MultiHopExpectedIDs []uuid.UUID `json:"multi_hop_expected_ids"`
	ObsoleteIDs         []uuid.UUID `json:"obsolete_ids"`
	ForbiddenIDs        []uuid.UUID `json:"forbidden_ids"`
}

// RunDataset executes the cognitive evaluation suite against golden_dataset.json.
func RunDataset(ctx context.Context, datasetPath string, maxTokens, hops int) (*Report, error) {
	raw, err := os.ReadFile(datasetPath)
	if err != nil {
		return nil, fmt.Errorf("read dataset %q: %w", datasetPath, err)
	}
	var ds goldenDataset
	if err := json.Unmarshal(raw, &ds); err != nil {
		return nil, fmt.Errorf("parse dataset %q: %w", datasetPath, err)
	}
	if len(ds.Queries) == 0 {
		return nil, fmt.Errorf("dataset %q contains no queries", datasetPath)
	}
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	if hops <= 0 {
		hops = 2
	}

	tenantID := uuid.New()
	projectID := uuid.New()
	callerAgent := uuid.New()
	foreignAgent := uuid.New()
	now := time.Now().UTC()

	repo := &evalSearchRepo{
		memories:   make(map[uuid.UUID]memory.Memory, len(ds.Memories)),
		embeddings: make(map[uuid.UUID][]float32, len(ds.Memories)),
	}
	for _, gm := range ds.Memories {
		owner := &callerAgent
		if gm.Owner == "foreign_agent" {
			owner = &foreignAgent
		}
		m := memory.Memory{
			ID:              gm.ID,
			TenantID:        tenantID,
			ProjectID:       &projectID,
			OwnerAgentID:    owner,
			Type:            gm.Type,
			ScopeType:       gm.ScopeType,
			Status:          gm.Status,
			TopicKey:        gm.TopicKey,
			Summary:         gm.Summary,
			Content:         gm.Content,
			Tags:            gm.Tags,
			EpistemicStatus: gm.EpistemicStatus,
			Confidence:      gm.Confidence,
			Importance:      gm.Importance,
			UtilityScore:    gm.UtilityScore,
			PrimarySourceID: uuid.New(),
			Clocks: memory.TriTemporalWindow{
				ObservedAt: now,
				ValidFrom:  now,
				CreatedAt:  now,
			},
		}
		repo.memories[m.ID] = m
		repo.embeddings[m.ID] = gm.DenseVec
	}
	for _, gr := range ds.Relations {
		repo.relations = append(repo.relations, memory.MemoryRelation{
			ID:           uuid.New(),
			TenantID:     tenantID,
			ProjectID:    &projectID,
			FromMemoryID: gr.FromID,
			ToMemoryID:   gr.ToID,
			RelationType: gr.RelationType,
			Weight:       gr.Weight,
			ValidFrom:    now,
			IsActive:     true,
			CreatedAt:    now,
		})
	}

	l1 := cachel1.NewCognitiveGraph(repo)
	embedder := &evalEmbedder{vecs: make(map[string][]float32, len(ds.Queries))}
	for _, q := range ds.Queries {
		embedder.vecs[strings.TrimSpace(q.Query)] = q.QueryVec
	}

	retSvc, err := retrieval.NewService(repo, repo, nil, nil, func() time.Time { return now })
	if err != nil {
		return nil, err
	}
	retSvc.SetHybridDeps(repo, embedder, l1)
	retSvc.WithReranker(neural.DeterministicReranker{}, governor.ProfileUltra)

	ctxSvc, err := contextbuilder.NewService(retSvc, repo, func() time.Time { return now })
	if err != nil {
		return nil, err
	}

	auth := identity.AuthContext{
		TenantID:        tenantID,
		PrincipalID:     uuid.New(),
		PrincipalType:   identity.PrincipalAgent,
		AgentID:         &callerAgent,
		AllowedProjects: []uuid.UUID{projectID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeProjectShared, identity.ScopeAgentPrivate},
	}

	var totalNDCG float64
	var multiHopExpected, multiHopFound int
	var obsoleteChecked, obsoleteLeaked int
	var forbiddenChecked, forbiddenLeaked int

	for _, q := range ds.Queries {
		res, err := retSvc.SearchMemories(ctx, auth, retrieval.SearchQuery{
			Query: q.Query,
			Limit: 10,
			Hops:  hops,
		})
		if err != nil {
			return nil, fmt.Errorf("query %q SearchMemories failed: %w", q.Name, err)
		}
		ctxRes, err := ctxSvc.BuildContext(ctx, auth, contextbuilder.BuildContextRequest{
			Query:     q.Query,
			MaxTokens: maxTokens,
			Hops:      hops,
		})
		if err != nil {
			return nil, fmt.Errorf("query %q BuildContext failed: %w", q.Name, err)
		}
		if ctxRes.UsedTokens > ctxRes.MaxTokens {
			return nil, fmt.Errorf("query %q exceeded max_tokens: %d > %d", q.Name, ctxRes.UsedTokens, ctxRes.MaxTokens)
		}

		returned := make(map[uuid.UUID]bool, len(res.Candidates))
		returnedOrder := make([]uuid.UUID, 0, len(res.Candidates))
		for _, c := range res.Candidates {
			returned[c.ID] = true
			returnedOrder = append(returnedOrder, c.ID)
		}
		for _, item := range ctxRes.Items {
			returned[item.MemoryID] = true
		}

		totalNDCG += computeNDCGAt5(returnedOrder, q.ExpectedTopIDs)

		for _, id := range q.MultiHopExpectedIDs {
			multiHopExpected++
			if returned[id] {
				multiHopFound++
			}
		}
		for _, id := range q.ObsoleteIDs {
			obsoleteChecked++
			if returned[id] {
				obsoleteLeaked++
			}
		}
		for _, id := range q.ForbiddenIDs {
			forbiddenChecked++
			if returned[id] {
				forbiddenLeaked++
			}
		}
	}

	ndcg := totalNDCG / float64(len(ds.Queries))
	multiHopRecall := 1.0
	if multiHopExpected > 0 {
		multiHopRecall = float64(multiHopFound) / float64(multiHopExpected)
	}
	obsoleteSuppression := 1.0
	if obsoleteChecked > 0 {
		obsoleteSuppression = 1.0 - (float64(obsoleteLeaked) / float64(obsoleteChecked))
	}
	leakage := 0.0
	if forbiddenChecked > 0 {
		leakage = float64(forbiddenLeaked) / float64(forbiddenChecked)
	}

	contradictionCatchRate, err := evaluateNLIContradictions(ctx, ds.NLIPairs)
	if err != nil {
		return nil, err
	}
	remDistillationRatio, err := evaluateREMConsolidation(ctx, tenantID, projectID, callerAgent, now, ds.REMEpisodes, ds.Memories)
	if err != nil {
		return nil, err
	}

	pass := ndcg >= 0.85 &&
		multiHopRecall == 1.0 &&
		obsoleteSuppression == 1.0 &&
		leakage == 0.0 &&
		contradictionCatchRate > 0.90 &&
		remDistillationRatio > 0.0
	return &Report{
		DatasetPath:             datasetPath,
		CasesEvaluated:          len(ds.Queries),
		NDCGAt5:                 ndcg,
		MultiHopRecall:          multiHopRecall,
		ObsoleteSuppressionRate: obsoleteSuppression,
		ZeroTrustLeakageRate:    leakage,
		ContradictionCatchRate:  contradictionCatchRate,
		REMDistillationRatio:    remDistillationRatio,
		Pass:                    pass,
	}, nil
}

func evaluateNLIContradictions(ctx context.Context, pairs []goldenNLIPair) (float64, error) {
	if len(pairs) == 0 {
		return 1.0, nil
	}
	nli := neural.DeterministicNLIProvider{}
	inputs := make([]memory.NLIPairInput, len(pairs))
	for i, p := range pairs {
		inputs[i] = memory.NLIPairInput{
			Premise:    p.Premise,
			Hypothesis: p.Hypothesis,
		}
	}
	scores, err := nli.EvaluatePairs(ctx, inputs)
	if err != nil {
		return 0, fmt.Errorf("evaluate deterministic NLI pairs: %w", err)
	}
	var expectedContradictions, caughtContradictions int
	for i, p := range pairs {
		decision := memory.EvaluateNLIDecision(
			memory.Memory{ID: uuid.New(), Summary: p.Premise},
			memory.Memory{ID: uuid.New(), Summary: p.Hypothesis},
			scores[i],
		)
		expected := strings.ToLower(strings.TrimSpace(p.ExpectedAction))
		switch expected {
		case string(memory.NLIDecisionContradicts):
			expectedContradictions++
			if decision.Action == memory.NLIDecisionContradicts {
				caughtContradictions++
			}
		case string(memory.NLIDecisionSupports):
			if decision.Action == memory.NLIDecisionContradicts {
				return 0, fmt.Errorf("nli pair %q falsely classified supports as contradicts", p.Name)
			}
		}
	}
	if expectedContradictions == 0 {
		return 1.0, nil
	}
	return float64(caughtContradictions) / float64(expectedContradictions), nil
}

func goldenToMemory(tenantID, projectID, callerAgent uuid.UUID, now time.Time, gm goldenMemory) memory.Memory {
	var ownerID *uuid.UUID
	if gm.Owner == "caller" {
		id := callerAgent
		ownerID = &id
	}
	pid := projectID
	return memory.Memory{
		ID:              gm.ID,
		TenantID:        tenantID,
		ProjectID:       &pid,
		OwnerAgentID:    ownerID,
		ScopeType:       gm.ScopeType,
		Type:            gm.Type,
		Status:          gm.Status,
		EpistemicStatus: gm.EpistemicStatus,
		Confidence:      gm.Confidence,
		Importance:      gm.Importance,
		UtilityScore:    gm.UtilityScore,
		TopicKey:        gm.TopicKey,
		Summary:         gm.Summary,
		Content:         gm.Content,
		Tags:            gm.Tags,
		UpdatedAt:       now,
	}
}

func evaluateREMConsolidation(
	ctx context.Context,
	tenantID, projectID, callerAgent uuid.UUID,
	now time.Time,
	episodes []goldenMemory,
	allMemories []goldenMemory,
) (float64, error) {
	if len(episodes) == 0 {
		return 0.0, nil
	}
	mems := make([]memory.Memory, 0, len(episodes))
	vecs := make(map[uuid.UUID][]float32, len(episodes))
	for _, gm := range episodes {
		mems = append(mems, goldenToMemory(tenantID, projectID, callerAgent, now, gm))
		if len(gm.DenseVec) > 0 {
			vecs[gm.ID] = gm.DenseVec
		}
	}
	verifiedMems := make([]memory.Memory, 0, len(allMemories))
	for _, gm := range allMemories {
		if gm.Status == memory.StatusActive && gm.EpistemicStatus == memory.EpistemicVerified {
			verifiedMems = append(verifiedMems, goldenToMemory(tenantID, projectID, callerAgent, now, gm))
		}
	}

	clusters := memory.ClusterEpisodicMemories(mems, vecs)
	if len(clusters) == 0 {
		return 0.0, nil
	}
	slm := &neural.DeterministicSLMConsolidator{}
	nli := neural.DeterministicNLIProvider{}
	distilledOK := 0
	for _, cluster := range clusters {
		draft, err := slm.DistillCluster(ctx, cluster.Episodes)
		if err != nil {
			return 0, fmt.Errorf("deterministic SLM distill cluster: %w", err)
		}
		draftText := strings.TrimSpace(draft.Summary + "\n" + draft.Content)
		relevantVerified := memory.FilterVerifiedForScope(verifiedMems, cluster.ScopeTarget)
		pairs := make([]memory.NLIPairInput, 0, len(cluster.Episodes)+len(relevantVerified))
		for _, ep := range cluster.Episodes {
			pairs = append(pairs, memory.NLIPairInput{
				Premise:    draftText,
				Hypothesis: strings.TrimSpace(ep.Summary + "\n" + ep.Content),
			})
		}
		for _, v := range relevantVerified {
			pairs = append(pairs, memory.NLIPairInput{
				Premise:    draftText,
				Hypothesis: strings.TrimSpace(v.Summary + "\n" + v.Content),
			})
		}
		scores, err := nli.EvaluatePairs(ctx, pairs)
		if err != nil {
			return 0, fmt.Errorf("deterministic NLI validate distilled draft: %w", err)
		}
		if len(scores) != len(pairs) {
			continue
		}
		sourceScores := scores[:len(cluster.Episodes)]
		verifiedScores := scores[len(cluster.Episodes):]
		if memory.ValidateDistilledNLI(sourceScores, verifiedScores) {
			distilledOK++
		}
	}
	return float64(distilledOK) / float64(len(clusters)), nil
}

func computeNDCGAt5(actual, expected []uuid.UUID) float64 {
	if len(expected) == 0 {
		return 1.0
	}
	relSet := make(map[uuid.UUID]bool, len(expected))
	for _, id := range expected {
		relSet[id] = true
	}
	limit := min(5, len(actual))
	var dcg float64
	for i := 0; i < limit; i++ {
		if relSet[actual[i]] {
			dcg += 1.0 / math.Log2(float64(i+2))
		}
	}
	idealLimit := min(5, len(expected))
	var idcg float64
	for i := 0; i < idealLimit; i++ {
		idcg += 1.0 / math.Log2(float64(i+2))
	}
	if idcg == 0 {
		return 1.0
	}
	return dcg / idcg
}

type evalEmbedder struct {
	vecs map[string][]float32
}

func (e *evalEmbedder) ModelName() string { return "BAAI/bge-m3-eval" }
func (e *evalEmbedder) Dimensions() int   { return 4 }
func (e *evalEmbedder) Embed(_ context.Context, text string) (memory.EmbeddingOutput, error) {
	if v, ok := e.vecs[strings.TrimSpace(text)]; ok {
		return memory.EmbeddingOutput{Dense: v}, nil
	}
	return memory.EmbeddingOutput{Dense: []float32{1, 0, 0, 0}}, nil
}
func (e *evalEmbedder) EmbedBatch(ctx context.Context, texts []string) ([]memory.EmbeddingOutput, error) {
	out := make([]memory.EmbeddingOutput, len(texts))
	for i, t := range texts {
		v, _ := e.Embed(ctx, t)
		out[i] = v
	}
	return out, nil
}

type evalSearchRepo struct {
	memories   map[uuid.UUID]memory.Memory
	relations  []memory.MemoryRelation
	embeddings map[uuid.UUID][]float32
}

func (r *evalSearchRepo) ExecTx(ctx context.Context, fn func(memory.RepositoryRegistry) error) error {
	return fn(r)
}
func (r *evalSearchRepo) Sources() memory.SourceRepository    { return nil }
func (r *evalSearchRepo) Memories() memory.MemoryRepository   { return r }
func (r *evalSearchRepo) Feedback() memory.FeedbackRepository { return nil }
func (r *evalSearchRepo) Jobs() memory.JobRepository          { return nil }
func (r *evalSearchRepo) Audit() memory.AuditLogger           { return r }
func (r *evalSearchRepo) RecordAllowed(context.Context, memory.AuditEvent) error {
	return nil
}
func (r *evalSearchRepo) RecordDenied(context.Context, memory.AuditEvent) error {
	return nil
}

func (r *evalSearchRepo) GetMemoryByID(_ context.Context, tenantID, id uuid.UUID) (*memory.Memory, error) {
	m, ok := r.memories[id]
	if !ok || m.TenantID != tenantID {
		return nil, memory.ErrNotFound
	}
	cp := m
	return &cp, nil
}
func (r *evalSearchRepo) GetMemoryByIdempotencyKey(context.Context, uuid.UUID, string) (*memory.Memory, error) {
	return nil, memory.ErrNotFound
}
func (r *evalSearchRepo) LockActiveTopicMemory(context.Context, identity.MemoryScopeTarget, string) (*memory.Memory, error) {
	return nil, memory.ErrNotFound
}
func (r *evalSearchRepo) InsertMemory(context.Context, memory.Memory) error { return nil }
func (r *evalSearchRepo) UpdateMemoryState(context.Context, memory.Memory) error {
	return nil
}
func (r *evalSearchRepo) ListVersionsByMemoryID(context.Context, uuid.UUID) ([]memory.MemoryVersion, error) {
	return nil, nil
}
func (r *evalSearchRepo) InsertMemoryVersion(context.Context, memory.MemoryVersion) error {
	return nil
}
func (r *evalSearchRepo) CloseMemoryVersionValidity(context.Context, uuid.UUID, int, time.Time) error {
	return nil
}
func (r *evalSearchRepo) GetMemoryVersionPointInTime(context.Context, uuid.UUID, time.Time, time.Time) (*memory.MemoryVersion, error) {
	return nil, memory.ErrNotFound
}
func (r *evalSearchRepo) ListActiveRelationsByMemoryID(context.Context, uuid.UUID, uuid.UUID) ([]memory.MemoryRelation, error) {
	return nil, nil
}
func (r *evalSearchRepo) InsertMemoryRelation(context.Context, memory.MemoryRelation) error {
	return nil
}

func (r *evalSearchRepo) SearchByTrigram(_ context.Context, f memory.SearchFilter) ([]uuid.UUID, error) {
	return r.lexicalMatch(f), nil
}
func (r *evalSearchRepo) SearchByFTS(_ context.Context, f memory.SearchFilter) ([]uuid.UUID, error) {
	return r.lexicalMatch(f), nil
}
func (r *evalSearchRepo) SearchByVector(_ context.Context, _ memory.SearchFilter, qVec []float32) ([]uuid.UUID, error) {
	type scored struct {
		id  uuid.UUID
		sim float64
	}
	var items []scored
	for id, v := range r.embeddings {
		sim := cosine(qVec, v)
		if sim > 0 {
			items = append(items, scored{id: id, sim: sim})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].sim > items[j].sim })
	out := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		out = append(out, it.id)
	}
	return out, nil
}
func (r *evalSearchRepo) GetMemoriesByIDs(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]memory.Memory, error) {
	out := make(map[uuid.UUID]memory.Memory, len(ids))
	for _, id := range ids {
		if m, ok := r.memories[id]; ok {
			out[id] = m
		}
	}
	return out, nil
}
func (r *evalSearchRepo) LoadProjectGraphSnapshot(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ time.Time) ([]memory.Memory, []memory.MemoryRelation, map[uuid.UUID][]float32, error) {
	mems := make([]memory.Memory, 0, len(r.memories))
	for _, m := range r.memories {
		mems = append(mems, m)
	}
	return mems, r.relations, r.embeddings, nil
}
func (r *evalSearchRepo) TraverseCausalSubgraph(context.Context, uuid.UUID, []uuid.UUID, int, time.Time) ([]memory.CausalWalkRow, error) {
	return nil, nil
}

func (r *evalSearchRepo) lexicalMatch(f memory.SearchFilter) []uuid.UUID {
	terms := strings.Fields(strings.ToLower(f.QueryText))
	type scored struct {
		id    uuid.UUID
		hits  int
		title string
	}
	var matched []scored
	for id, m := range r.memories {
		hay := strings.ToLower(m.TopicKey + " " + m.Summary + " " + m.Content)
		hits := 0
		for _, t := range terms {
			if len(t) >= 3 && strings.Contains(hay, t) {
				hits++
			}
		}
		if hits > 0 {
			matched = append(matched, scored{id: id, hits: hits, title: m.Summary})
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].hits != matched[j].hits {
			return matched[i].hits > matched[j].hits
		}
		return matched[i].title < matched[j].title
	})
	out := make([]uuid.UUID, 0, len(matched))
	for _, m := range matched {
		out = append(out, m.id)
	}
	return out
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		fa, fb := float64(a[i]), float64(b[i])
		dot += fa * fb
		na += fa * fa
		nb += fb * fb
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
