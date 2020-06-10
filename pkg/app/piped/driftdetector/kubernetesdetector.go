// Copyright 2020 The PipeCD Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package driftdetector

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/kapetaniosci/pipe/pkg/app/piped/livestatestore/kubernetes"
	"github.com/kapetaniosci/pipe/pkg/cache"
	"github.com/kapetaniosci/pipe/pkg/config"
	"github.com/kapetaniosci/pipe/pkg/git"
	"github.com/kapetaniosci/pipe/pkg/model"
)

type kubernetesDetector struct {
	provider          config.PipedCloudProvider
	appLister         applicationLister
	gitClient         gitClient
	stateGetter       kubernetes.Getter
	apiClient         apiClient
	appManifestsCache cache.Cache
	interval          time.Duration
	config            *config.PipedSpec
	logger            *zap.Logger

	gitRepos map[string]git.Repo
}

func newKubernetesDetector(
	cp config.PipedCloudProvider,
	appLister applicationLister,
	gitClient gitClient,
	stateGetter kubernetes.Getter,
	apiClient apiClient,
	appManifestsCache cache.Cache,
	cfg *config.PipedSpec,
	logger *zap.Logger,
) *kubernetesDetector {

	logger = logger.Named("kubernetes-detector").With(
		zap.String("cloud-provider", cp.Name),
	)
	return &kubernetesDetector{
		provider:          cp,
		appLister:         appLister,
		gitClient:         gitClient,
		stateGetter:       stateGetter,
		apiClient:         apiClient,
		appManifestsCache: appManifestsCache,
		interval:          time.Minute,
		config:            cfg,
		gitRepos:          make(map[string]git.Repo),
		logger:            logger,
	}
}

func (d *kubernetesDetector) Run(ctx context.Context) error {
	d.logger.Info("start running drift detector for kubernetes applications")

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

L:
	for {
		select {

		case <-ticker.C:
			d.check(ctx)

		case <-ctx.Done():
			break L
		}
	}

	d.logger.Info("drift detector for kubernetes applications has been stopped")
	return nil
}

func (d *kubernetesDetector) check(ctx context.Context) error {
	var (
		err          error
		applications = d.listApplications()
	)

	for repoID, apps := range applications {
		gitRepo, ok := d.gitRepos[repoID]
		if !ok {
			// Clone repository for the first time.
			repoCfg, ok := d.config.GetRepository(repoID)
			if !ok {
				d.logger.Error(fmt.Sprintf("repository %s was not found in piped configuration", repoID))
				continue
			}
			gitRepo, err = d.gitClient.Clone(ctx, repoID, repoCfg.Remote, repoCfg.Branch, "")
			if err != nil {
				d.logger.Error("failed to clone repository",
					zap.String("repo-id", repoID),
					zap.Error(err),
				)
				continue
			}
			d.gitRepos[repoID] = gitRepo
		}

		// Fetch to update the repository.
		branch := gitRepo.GetClonedBranch()
		if err := gitRepo.Pull(ctx, branch); err != nil {
			d.logger.Error("failed to update repository branch",
				zap.String("repo-id", repoID),
				zap.Error(err),
			)
			continue
		}

		// Get the head commit of the repository.
		headCommit, err := gitRepo.GetLatestCommit(ctx)
		if err != nil {
			d.logger.Error("failed to get head commit hash",
				zap.String("repo-id", repoID),
				zap.Error(err),
			)
			continue
		}

		for _, app := range apps {
			if err := d.checkApplication(ctx, app, gitRepo, headCommit); err != nil {
				d.logger.Error(fmt.Sprintf("failed to check application: %s", app.Id), zap.Error(err))
			}
		}
	}

	return nil
}

func (d *kubernetesDetector) checkApplication(ctx context.Context, app *model.Application, repo git.Repo, headCommit git.Commit) error {
	liveManifests := d.stateGetter.GetAppLiveManifests(app.Id)
	d.logger.Info(fmt.Sprintf("application %s has %d live manifests", app.Id, len(liveManifests)))
	d.logger.Info(fmt.Sprintf("the latest commit is %s", headCommit.Hash))
	return nil
}

// listApplications retrieves all applications those should be handled by this director
// and then groups them by repoID.
func (d *kubernetesDetector) listApplications() map[string][]*model.Application {
	var (
		apps = d.appLister.ListByCloudProvider(d.provider.Name)
		m    = make(map[string][]*model.Application)
	)
	for _, app := range apps {
		repoID := app.GitPath.RepoId
		if _, ok := m[repoID]; !ok {
			m[repoID] = []*model.Application{app}
		} else {
			m[repoID] = append(m[repoID], app)
		}
	}
	return m
}

func (d *kubernetesDetector) ProviderName() string {
	return d.provider.Name
}
