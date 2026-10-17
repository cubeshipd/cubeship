package objectstore

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"cubeship/internal/user"
)

// RotateCredentials changes the managed S3 login in SeaweedFS before
// recording it, then restores the old login if persistence fails.
func (s *Service) RotateCredentials(ctx context.Context, caller *user.User, name string) (Credentials, error) {
	store, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return Credentials{}, err
	}
	if store.Kind != KindManaged {
		return Credentials{}, ErrLinkedHasNoContainer
	}
	if err := user.AllowSecrets(caller, user.ResStorage, name); err != nil {
		return Credentials{}, err
	}
	mu := s.prov.lock(store.ID)
	mu.Lock()
	defer mu.Unlock()

	store, err = s.Repo().ByID(ctx, store.ID)
	if err != nil {
		return Credentials{}, err
	}
	if store.ContainerID == "" || store.Status != StatusRunning {
		return Credentials{}, ErrNotRunning
	}

	accessKey, secretKey, err := GenerateKeys()
	if err != nil {
		return Credentials{}, err
	}
	if err := s.applyManagedCredentials(ctx, store, accessKey, secretKey); err != nil {
		return Credentials{}, fmt.Errorf("change object store credentials: %w", err)
	}
	if err := s.Repo().SetManagedCredentials(ctx, store.ID, accessKey, secretKey); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if rollbackErr := s.applyManagedCredentials(rollbackCtx, store, store.AccessKey, store.SecretKey); rollbackErr != nil {
			return Credentials{}, fmt.Errorf("save rotated credentials: %v; restore the previous credentials: %w", err, rollbackErr)
		}
		return Credentials{}, err
	}
	return s.Credentials(ctx, caller, name)
}

func (s *Service) applyManagedCredentials(ctx context.Context, store *Store, accessKey, secretKey string) error {
	input := managedCredentialRotationInput(accessKey, secretKey)
	_, code, err := s.prov.docker.ExecStream(ctx, store.ContainerID,
		[]string{"weed", "shell", "-master=localhost:9333", "-filer=localhost:8888"},
		strings.NewReader(input), io.Discard)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("weed shell exited with status %d", code)
	}
	return nil
}

func managedCredentialRotationInput(accessKey, secretKey string) string {
	return fmt.Sprintf("s3.configure -access_key=%s -secret_key=%s -user=mini -actions=Admin,Read,List,Tagging,Write -apply\n", accessKey, secretKey)
}
