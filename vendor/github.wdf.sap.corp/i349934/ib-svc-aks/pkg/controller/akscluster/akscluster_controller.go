package akscluster

import (
	"context"
	"log"

	"github.com/sirupsen/logrus"

	azurev1alpha1 "github.wdf.sap.corp/i349934/ib-svc-aks/aks/pkg/apis/azure/v1alpha1"
	"github.com/satori/go.uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"github.wdf.sap.corp/i349934/ib-svc-aks/pkg/apis/azure/v1alpha1"
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new AKSCluster Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileAKSCluster{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("akscluster-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource AKSCluster
	err = c.Watch(&source.Kind{Type: &azurev1alpha1.AKSCluster{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileAKSCluster{}

// ReconcileAKSCluster reconciles a AKSCluster object
type ReconcileAKSCluster struct {
	// TODO: Clarify the split client
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a AKSCluster object and makes changes based on the state read
// and what is in the AKSCluster.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileAKSCluster) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.Printf("Reconciling AKSCluster %s/%s\n", request.Namespace, request.Name)

	// Fetch the AKSCluster instance
	cr := &azurev1alpha1.AKSCluster{}
	err := r.client.Get(context.TODO(), request.NamespacedName, cr)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	log := logrus.WithFields(logrus.Fields{
		"namespace": cr.Namespace,
		"name":      cr.Name,
	})

	delTimestamp := cr.GetDeletionTimestamp()
	if delTimestamp != nil {
		deleteAKSCluster(r, cr, log)
	} else {
		finalizers := cr.GetFinalizers()
		if len(finalizers) == 0 {
			cr.SetFinalizers([]string{"azure.service.infrabox.net"})
			cr.Status.Status = "pending"
			u := uuid.NewV4()
			cr.Status.ClusterName = "ib-" + u.String()
			err := r.client.Update(context.TODO() , cr)
			if err != nil {
				log.Errorf("Failed to set finalizers: %v", err)
				return reconcile.Result{}, nil
			}
		}
	}

	return reconcile.Result{}, nil
}

func deleteAKSCluster(r *ReconcileAKSCluster, cr *v1alpha1.AKSCluster, log *logrus.Entry) error {
	cr.Status.Status = "pending"
	cr.Status.Message = "deleting"

	err := r.client.Update(context.TODO(), cr)
	if err != nil {
		log.Errorf("Failed to update status: %v", err)
		return err
	}

	//TODO: get cluster  status
	//TODO: delete cluster
	//TODO: delete secret

	cr.SetFinalizers([]string{})
	err = r.client.Update(context.TODO(), cr)
	if err != nil {
		log.Errorf("Failed to remove finalizers: %v", err)
		return err
	}

	err = r.client.Delete(context.TODO(), cr)
	if err != nil && !errors.IsNotFound(err) {
		log.Errorf("Failed to delete cr: %v", err)
		return err
	}

	log.Info("AKSCluster deleted")

	return nil
}

// newPodForCR returns a busybox pod with the same name/namespace as the cr
