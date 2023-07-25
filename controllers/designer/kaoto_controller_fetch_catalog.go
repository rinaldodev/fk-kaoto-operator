package designer

import (
	"context"

	"gopkg.in/yaml.v3"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type catalogAction struct {
}

type Catalog struct {
	Objects []CatalogItem `yaml:"objects"`
}

type CatalogItem struct {
	Data     map[string]string `yaml:"data"`
	Metadata struct {
		Name string `yaml:"name"`
	}
}

func (a *catalogAction) Apply(ctx context.Context, rr *ReconciliationRequest) error {
	catalogCondition := metav1.Condition{
		Type:               "Catalog",
		Status:             metav1.ConditionTrue,
		Reason:             "Deployed",
		Message:            "Deployed",
		ObservedGeneration: rr.Kaoto.Generation,
	}

	err := a.catalog(ctx, rr)
	if err != nil {
		catalogCondition.Status = metav1.ConditionFalse
		catalogCondition.Reason = "Failure"
		catalogCondition.Message = err.Error()
	}

	meta.SetStatusCondition(&rr.Kaoto.Status.Conditions, catalogCondition)

	return err
}

func (a *catalogAction) catalog(ctx context.Context, rr *ReconciliationRequest) error {
	repo, err := remote.NewRepository(rr.Kaoto.Spec.CatalogRepo)
	if err != nil {
		return err
	}

	repo.PlainHTTP = true

	descriptor, err := repo.Blobs().Resolve(ctx, rr.Kaoto.Spec.CatalogReference)
	if err != nil {
		return err
	}

	rc, err := repo.Fetch(ctx, descriptor)
	if err != nil {
		return err
	}

	defer rc.Close()

	pulledBlob, err := content.ReadAll(rc, descriptor)
	if err != nil {
		return err
	}

	var catalog Catalog
	err = yaml.Unmarshal(pulledBlob, &catalog)
	if err != nil {
		return err
	}

	for _, object := range catalog.Objects {
		err = reify(
			ctx,
			rr,
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rr.Kaoto.Name + "-" + object.Metadata.Name,
					Namespace: rr.Kaoto.Namespace,
				},
			},
			func(resource *corev1.ConfigMap) (*corev1.ConfigMap, error) {
				if err := controllerutil.SetControllerReference(rr.Kaoto, resource, rr.Scheme()); err != nil {
					return resource, errors.New("unable to set controller reference")
				}
				resource.Data = make(map[string]string)
				for k, v := range object.Data {
					resource.Data[k] = v
				}

				return resource, nil
			},
		)
		if err != nil {
			break
		}
	}

	return err
}
