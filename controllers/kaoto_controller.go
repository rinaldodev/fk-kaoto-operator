/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"

	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kaotoiov1alpha1 "github.com/kaotoIO/kaoto-operator/api/v1alpha1"
)

// KaotoReconciler reconciles a Kaoto object
type KaotoReconciler struct {
	KaotoParams KaotoParams
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=kaoto.io,namespace=kaoto-operator,resources=kaotoes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kaoto.io,namespace=kaoto-operator,resources=kaotoes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kaoto.io,namespace=kaoto-operator,resources=kaotoes/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,namespace=kaoto-operator,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",namespace=kaoto-operator,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="route.openshift.io",namespace=kaoto-operator,resources=routes,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Kaoto object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *KaotoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	kaoto := &kaotoiov1alpha1.Kaoto{}
	err := r.Get(ctx, req.NamespacedName, kaoto)
	if err != nil {
		if errors.IsNotFound(err) {
			// no CR found
			return ctrl.Result{}, nil
		}
	}

	//check the frontend deployment
	frontendDep := &appsv1.Deployment{}

	err = r.Get(ctx, types.NamespacedName{Name: r.KaotoParams.FrontendName, Namespace: kaoto.Namespace}, frontendDep)
	if err != nil && errors.IsNotFound(err) {
		frontendDep = GetFrontEndDeployment(r.KaotoParams, *kaoto)
		_ = ctrl.SetControllerReference(kaoto, frontendDep, r.Scheme)

		err = r.Create(ctx, frontendDep)
		if err != nil {
			log.Error(err, "Failed to create Deployment for the frontend", "Deployment.Namespace", frontendDep.Namespace, "Deployment.Name", frontendDep.Name)
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}
	// frontEndService
	frontendService := &v1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: r.KaotoParams.FrontendName + "-svc", Namespace: kaoto.Namespace}, frontendService)
	if err != nil && errors.IsNotFound(err) {
		frontendService = NewService(kaoto.Name, r.KaotoParams.FrontendName, kaoto.Namespace, 80, r.KaotoParams.FrontendPort)
		_ = ctrl.SetControllerReference(kaoto, frontendService, r.Scheme)
		err = r.Create(ctx, frontendService)
		if err != nil {
			log.Error(err, "failed to create frontend service")
			return ctrl.Result{}, err
		} else {
			log.Info("the backend service "+frontendService.Name+"was created", "Kaoto.Service.Frontend", kaoto.Namespace)
		}
	}
	// frontEndRoute
	kaotoRoute := &routev1.Route{}
	err = r.Get(ctx, types.NamespacedName{Name: kaoto.Name, Namespace: kaoto.Namespace}, kaotoRoute)

	if err != nil && errors.IsNotFound(err) {
		kaotoRoute = NewRoute(kaoto.Name, kaoto.Name, *frontendService)
		_ = ctrl.SetControllerReference(kaoto, kaotoRoute, r.Scheme)
		err = r.Create(ctx, kaotoRoute)
		if err != nil {
			log.Error(err, "failed to create Route")
			return ctrl.Result{}, err
		} else {
			log.Info("the kaoto route "+kaotoRoute.Name+"was created", "Kaoto.Route", kaoto.Namespace)
		}
	}

	// check the backend deployment
	backendDep := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: r.KaotoParams.BackendName, Namespace: kaoto.Namespace}, backendDep)
	if err != nil && errors.IsNotFound(err) {
		backendDep = GetBackendDeployment(r.KaotoParams, *kaoto, *kaotoRoute)
		_ = ctrl.SetControllerReference(kaoto, backendDep, r.Scheme)
		err = r.Create(ctx, backendDep)

		if err != nil {
			log.Error(err, "failed to create Deployment for the backend", "Deployment.Namespace", backendDep.Namespace, "Deployment.Name", backendDep.Name)
			return ctrl.Result{}, err
		} else {
			log.Info("the backend deployment was created", "Kaoto.Deployment.Backend", kaoto.Namespace, "Deployment.Name", backendDep.Name)
		}
	} else if err != nil {
		return ctrl.Result{}, err
	}
	// backEndService
	backendService := &v1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: r.KaotoParams.BackendName + "-svc", Namespace: kaoto.Namespace}, backendService)
	if err != nil && errors.IsNotFound(err) {
		backendService = NewService(kaoto.Name, r.KaotoParams.BackendName, kaoto.Namespace, r.KaotoParams.BackendPort, r.KaotoParams.BackendPort)
		_ = ctrl.SetControllerReference(kaoto, backendService, r.Scheme)
		err = r.Create(ctx, backendService)

		if err != nil {
			log.Error(err, "failed to create backend service")
			return ctrl.Result{}, err
		} else {
			log.Info("the backend service "+backendService.Name+"was created", "Kaoto.Service.Backend", kaoto.Namespace)
		}
	}
	log.Info("backend dep" + backendDep.Spec.Template.Spec.ServiceAccountName)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KaotoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kaotoiov1alpha1.Kaoto{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
