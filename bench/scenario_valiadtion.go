package bench

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/isucon/isucandar"
)

// ベンチ実行後の整合性検証シナリオ
// isucandar.ValidateScenarioを満たすメソッド
// isucandar.Benchmark の validation ステップで実行される
func (sc *Scenario) ValidationScenario(ctx context.Context, step *isucandar.BenchmarkStep) error {
	report := timeReporter("validation")
	defer report()

	ContestantLogger.Println("[ValidationScenario] 整合性チェックを開始します")
	defer ContestantLogger.Printf("[ValidationScenario] 整合性チェックを終了します")

	// 検証で作成する参加者数 結果のScoreも同数作成する
	playerNum := 20

	// SaaS管理者, 主催者, 参加者のagent作成
	admin := Account{
		Role:       AccountRoleAdmin,
		TenantName: "admin",
		PlayerID:   "admin",
		Option:     sc.Option,
	}
	if err := admin.SetJWT(sc.RawKey); err != nil {
		return err
	}
	adminAg, err := admin.GetAgent()
	if err != nil {
		return err
	}

	// SaaS管理API
	tenantName := "valid-tenantid"
	tenantDisplayName := "valid-Tenantname"
	{
		res, err := PostAdminTenantsAddAction(ctx, tenantName, tenantDisplayName, adminAg)
		v := ValidateResponse("新規テナント作成", step, res, err, WithStatusCode(200),
			WithSuccessResponse(func(r ResponseAPITenantsAdd) error {
				if tenantDisplayName != r.Data.Tenant.DisplayName {
					return fmt.Errorf("作成したテナントのDisplayNameが違います (want: %s, got: %s)", tenantDisplayName, r.Data.Tenant.DisplayName)
				}
				if tenantName != r.Data.Tenant.Name {
					return fmt.Errorf("作成したテナントのNameが違います (want: %s, got: %s)", tenantName, r.Data.Tenant.Name)
				}
				return nil
			}),
		)
		if !v.IsEmpty() {
			return v
		}

		// テナント追加 不正リクエストチェック
		invalidNames := map[string]int{
			"valid-tenantid":   http.StatusBadRequest, // 重複するname
			"INVALID_TENANTID": http.StatusBadRequest, // 不正なname
		}
		for name, code := range invalidNames {
			res, err := PostAdminTenantsAddAction(ctx, name, tenantDisplayName, adminAg)
			v := ValidateResponse("新規テナント作成 不正リクエスト", step, res, err, WithStatusCode(code))
			if !v.IsEmpty() {
				return v
			}
		}
	}

	// 大会主催者API
	organizer := Account{
		Role:       AccountRoleOrganizer,
		TenantName: tenantName,
		PlayerID:   "organizer",
		Option:     sc.Option,
	}
	if err := organizer.SetJWT(sc.RawKey); err != nil {
		return err
	}
	orgAg, err := organizer.GetAgent()
	if err != nil {
		return err
	}

	// プレイヤー追加
	var playerIDs []string
	var playerDisplayNames []string
	for i := 0; i < playerNum; i++ {
		playerDisplayNames = append(playerDisplayNames, fmt.Sprintf("validate_player%d", i))
	}
	{
		res, err := PostOrganizerPlayersAddAction(ctx, playerDisplayNames, orgAg)
		v := ValidateResponse("テナントへプレイヤー追加", step, res, err, WithStatusCode(200),
			WithSuccessResponse(func(r ResponseAPIPlayersAdd) error {
				if playerNum != len(r.Data.Players) {
					return fmt.Errorf("追加されたプレイヤー数が違います (want: %d, got: %d)", playerNum, len(r.Data.Players))
				}
				for _, pl := range r.Data.Players {
					if pl.IsDisqualified {
						return fmt.Errorf("新規追加されたプレイヤーは失格になっていない必要があります player.id: %s", pl.ID)
					}
					var ok bool
					for _, n := range playerDisplayNames {
						if n == pl.DisplayName {
							ok = true
							break
						}
					}
					if !ok {
						return fmt.Errorf("新規追加したプレイヤーのDisplayNameが見つかりません")
					}
					playerIDs = append(playerIDs, pl.ID)
				}
				return nil
			}),
		)
		if !v.IsEmpty() {
			return v
		}
		// NOTE: 不正リクエストチェックなし
	}

	// プレイヤー一覧取得
	{
		res, err := GetOrganizerPlayersListAction(ctx, orgAg)
		v := ValidateResponse("テナントのプレイヤー一覧取得", step, res, err, WithStatusCode(200),
			WithSuccessResponse(func(r ResponseAPIPlayersList) error {
				if len(playerIDs) != len(r.Data.Players) {
					return fmt.Errorf("プレイヤー数が違います (want: %d, got: %d)", playerNum, len(r.Data.Players))
				}
				for _, pl := range r.Data.Players {
					if pl.IsDisqualified {
						return fmt.Errorf("新規追加されたプレイヤーは失格になっていない必要があります player.id: %s", pl.ID)
					}
					var ok bool
					for _, id := range playerIDs {
						if id == pl.ID {
							ok = true
							break
						}
					}
					if !ok {
						return fmt.Errorf("新規追加されたプレイヤーが一覧に含まれていません: id:%s display_name:%s", pl.ID, pl.DisplayName)
					}
				}
				return nil
			}),
		)
		if !v.IsEmpty() {
			return v
		}
		// NOTE: 不正リクエストチェックなし
	}

	competitionName := "validate_competition"
	var competitionId string
	{
		res, err := PostOrganizerCompetitonsAddAction(ctx, competitionName, orgAg)
		v := ValidateResponse("新規大会追加", step, res, err, WithStatusCode(200),
			WithSuccessResponse(func(r ResponseAPICompetitionsAdd) error {
				if competitionName != r.Data.Competition.Title {
					return fmt.Errorf("追加された大会の名前が違います (want: %s, got: %s)", competitionName, r.Data.Competition.Title)
				}
				if r.Data.Competition.IsFinished {
					return fmt.Errorf("新規追加された大会は開催中である必要があります competition.title: %s, competition.id: %s", r.Data.Competition.Title, r.Data.Competition.ID)
				}
				competitionId = r.Data.Competition.ID
				return nil
			}),
		)
		if !v.IsEmpty() {
			return v
		}
		// NOTE: 不正リクエストチェックなし
	}
	{
		res, err := PostOrganizerApiPlayerDisqualifiedAction(ctx, playerIDs[1], orgAg)
		v := ValidateResponse("プレイヤーを失格にする", step, res, err, WithStatusCode(200),
			WithSuccessResponse(func(r ResponseAPIPlayerDisqualified) error {
				if !r.Data.Player.IsDisqualified {
					return fmt.Errorf("プレイヤーが失格になっていません player.id: %s", r.Data.Player.ID)
				}
				if playerIDs[1] != r.Data.Player.ID {
					return fmt.Errorf("失格にしたプレイヤーが違います (want: %s, got: %s)", playerIDs[1], r.Data.Player.ID)
				}
				return nil
			}),
		)
		if !v.IsEmpty() {
			return v
		}
		// TOOD: 不正リクエストチェック
		// - 存在しないプレイヤー
	}

	// 大会結果CSV入稿
	var score ScoreRows
	{
		// NOTE: 失格済みのプレイヤーは含まれていても問題ない
		for i, playerID := range playerIDs {
			score = append(score, &ScoreRow{
				PlayerID: playerID,
				Score:    100 * i,
			})
		}
		csv := score.CSV()
		res, err := PostOrganizerCompetitionResultAction(ctx, competitionId, []byte(csv), orgAg)
		v := ValidateResponse("大会結果CSV入稿", step, res, err, WithStatusCode(200),
			WithSuccessResponse(func(r ResponseAPICompetitionResult) error {
				_ = r // responseは空
				return nil
			}),
		)
		if !v.IsEmpty() {
			return v
		}

		// 不正リクエストチェック
		// - 存在しない大会
		res, err = PostOrganizerCompetitionResultAction(ctx, "nonexisting-competition", []byte(csv), orgAg)
		v = ValidateResponse("大会結果CSV入稿: 不正リクエスト(存在しない大会)", step, res, err, WithStatusCode(400))
		if !v.IsEmpty() {
			return v
		}

		// - 存在しないプレイヤーが含まれるCSVを入稿
		invalidScore := ScoreRows{&ScoreRow{
			PlayerID: "not-exist-player",
			Score:    1,
		}}
		invalidCSV := invalidScore.CSV()
		res, err = PostOrganizerCompetitionResultAction(ctx, competitionId, []byte(invalidCSV), orgAg)
		v = ValidateResponse("大会結果CSV入稿", step, res, err, WithStatusCode(400))
		if !v.IsEmpty() {
			return v
		}
	}
	{
		res, err := PostOrganizerCompetitionFinishAction(ctx, competitionId, orgAg)
		v := ValidateResponse("大会終了", step, res, err, WithStatusCode(200),
			WithSuccessResponse(func(r ResponseAPICompetitionRankingFinish) error {
				_ = r // responseは空
				return nil
			}),
		)
		if !v.IsEmpty() {
			return v
		}
		// TODO: 不正リクエストチェック
		// - 存在しない大会
	}
	{
		res, err := GetOrganizerBillingAction(ctx, orgAg)
		v := ValidateResponse("テナント内の請求情報", step, res, err, WithStatusCode(200),
			WithSuccessResponse(func(r ResponseAPIBilling) error {
				if 1 != len(r.Data.Reports) {
					return fmt.Errorf("請求レポートの数が違います (want: %d, got: %d)", 1, len(r.Data.Reports))
				}
				if competitionId != r.Data.Reports[0].CompetitionID {
					return fmt.Errorf("対象の大会のIDが違います (want: %s, got: %s)", competitionId, r.Data.Reports[0].CompetitionID)
				}
				return nil
			}),
		)
		if !v.IsEmpty() {
			return v
		}
		// NOTE: 不正リクエストチェックなし
	}

	// 大会参加者API
	player := Account{
		Role:       AccountRolePlayer,
		TenantName: tenantName,
		PlayerID:   playerIDs[0],
		Option:     sc.Option,
	}
	if err := player.SetJWT(sc.RawKey); err != nil {
		return err
	}
	playerAg, err := player.GetAgent()
	if err != nil {
		return err
	}

	{
		res, err := GetPlayerAction(ctx, playerIDs[0], playerAg)
		v := ValidateResponse("プレイヤーと戦績情報取得", step, res, err, WithStatusCode(200),
			WithSuccessResponse(func(r ResponseAPIPlayer) error {
				if playerIDs[0] != r.Data.Player.ID {
					return fmt.Errorf("参照したプレイヤー名が違います (want: %s, got: %s)", playerIDs[0], r.Data.Player.ID)
				}
				if 1 != len(r.Data.Scores) {
					return fmt.Errorf("参加した大会数が違います (want: %d, got: %d)", 1, len(r.Data.Scores))
				}
				// TODO: ランキングチェック
				return nil
			}),
		)
		if !v.IsEmpty() {
			return v
		}
		// TODO: 不正リクエストチェック
		// - 存在しないプレイヤー
	}
	{
		//rank_after未指定
		res, err := GetPlayerCompetitionRankingAction(ctx, competitionId, "", playerAg)
		v := ValidateResponse("大会内のランキング取得", step, res, err, WithStatusCode(200),
			WithSuccessResponse(func(r ResponseAPICompetitionRanking) error {
				if len(score) != len(r.Data.Ranks) && 100 < len(r.Data.Ranks) {
					return fmt.Errorf("大会のランキングの結果の数が違います(最大100件) (want: %d, got: %d)", len(score), len(r.Data.Ranks))
				}
				// TODO: ランキングの順序が正しいことを確認
				return nil
			}),
		)
		if !v.IsEmpty() {
			return v
		}
	}
	{
		// rank_afterで最後の1件だけを取るように指定する
		res, err := GetPlayerCompetitionRankingAction(ctx, competitionId, strconv.Itoa(len(score)-1), playerAg)
		v := ValidateResponse("大会内のランキング取得", step, res, err, WithStatusCode(200),
			WithSuccessResponse(func(r ResponseAPICompetitionRanking) error {
				if 1 != len(r.Data.Ranks) {
					return fmt.Errorf("rank_after指定時の大会のランキングの結果の数が違います (want: %d, got: %d)", 1, len(r.Data.Ranks))
				}
				return nil
			}),
		)
		if !v.IsEmpty() {
			return v
		}

		// TODO: 不正リクエストチェック
		// - 存在しない大会
	}
	{
		res, err := GetPlayerCompetitionsAction(ctx, playerAg)
		v := ValidateResponse("テナント内の大会情報取得", step, res, err, WithStatusCode(200),
			WithSuccessResponse(func(r ResponseAPICompetitions) error {
				if 1 != len(r.Data.Competitions) {
					return fmt.Errorf("テナントに含まれる大会の数が違います (want: %d, got: %d)", 1, len(r.Data.Competitions))
				}
				return nil
			}),
		)
		if !v.IsEmpty() {
			return v
		}
		// NOTE: 不正なリクエストチェックはなし
	}

	{
		// ページング無しで今回操作したテナントが含まれていることを確認
		res, err := GetAdminTenantsBillingAction(ctx, "", adminAg)
		v := ValidateResponse("テナント別の請求ダッシュボード", step, res, err, WithStatusCode(200),
			WithSuccessResponse(func(r ResponseAPITenantsBilling) error {
				if 1 > len(r.Data.Tenants) {
					return fmt.Errorf("請求ダッシュボードの結果が足りません")
				}
				tenantNameMap := make(map[string]struct{})
				for _, tenant := range r.Data.Tenants {
					tenantNameMap[tenant.DisplayName] = struct{}{}
				}
				if _, ok := tenantNameMap[tenantDisplayName]; !ok {
					return fmt.Errorf("請求ダッシュボードの結果に作成したテナントがありません")
				}
				return nil
			}),
		)
		if !v.IsEmpty() {
			return v
		}
		// TODO: リクエストチェック
		// - ページングありを検証するか要検討
	}

	return nil
}
