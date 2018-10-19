package akscluster

import (
	"context"
	"log"

	"github.com/sirupsen/logrus"

	"github.wdf.sap.corp/i349934/ib-svc-aks/pkg/apis/azure/v1alpha1"
	"github.com/satori/go.uuid"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"os/exec"
	"encoding/json"
	"strconv"
	"time"
	"strings"
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

type RemoteCluster struct {
	Name     string	`json:"name"`
	Status   string `json:"provisioningState"`
	Endpoint string `json:"fqdn"`
}

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
	err = c.Watch(&source.Kind{Type: &v1alpha1.AKSCluster{}}, &handler.EnqueueRequestForObject{})
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
	cr := &v1alpha1.AKSCluster{}
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
		status, err := syncAKSCluster(r, cr, log)

		if err != nil {
			cr.Status.Status = "error"
			cr.Status.Message = err.Error()
			err = r.client.Update(context.TODO(), cr)
			return reconcile.Result{}, err
		} else {
			log.Info("status:", status.Status, "msg:", status.Message)
			if cr.Status.Status != status.Status || cr.Status.Message != status.Message {
				cr.Status = *status
				err = r.client.Update(context.TODO(), cr)
				return reconcile.Result{Requeue:true, RequeueAfter:time.Duration(10 * time.Second)}, err
			}
		}
	}

	return reconcile.Result{}, nil
}

func syncAKSCluster(r *ReconcileAKSCluster, cr *v1alpha1.AKSCluster, log *logrus.Entry) (*v1alpha1.AKSClusterStatus, error) {
	if cr.Status.Status == "ready" || cr.Status.Status == "error" {
		return &cr.Status, nil
	}

	finalizers := cr.GetFinalizers()
	if len(finalizers) == 0 {
		cr.SetFinalizers([]string{"azure.service.infrabox.net"})
		cr.Status.Status = "pending"
		u := uuid.NewV4()
		cr.Status.ClusterName = "ib-" + u.String()
		err := r.client.Update(context.TODO(), cr)
		if err != nil {
			log.Errorf("Failed to set finalizers: %v", err)
			return nil, nil
		}
	}

	aksCluster, err := getRemoteCluster(cr.Status.ClusterName, log)
	if err != nil && !errors.IsNotFound(err) {
		log.Errorf("Could not get AKS Cluster: %v", err)
		return nil, err
	}

	if aksCluster == nil {
		args := []string{"aks", "create",
			"name", cr.Status.ClusterName,
			"--no-wait",
			"--no-ssh-key",
			"--location", cr.Spec.Zone,
		}

		if cr.Spec.DiskSize != 0 {
			args = append(args, "--node-osdisk-size")
			args = append(args, strconv.Itoa(int(cr.Spec.DiskSize)))
		}

		if cr.Spec.MachineType != "" {
			args = append(args, "--node-vm-size")
			args = append(args, cr.Spec.MachineType)
		}

		if cr.Spec.NumNodes != 0 {
			args = append(args, "--node-count")
			args = append(args, strconv.Itoa(int(cr.Spec.NumNodes)))
		}

		if cr.Spec.ClusterVersion != "" {
			args = append(args, "--kubernetes-version")
			args = append(args, cr.Spec.MachineType)
		}

		cmd := exec.Command("az", args...)
		out, err := cmd.Output()

		if err != nil {
			log.Errorf("Failed to create AKS Cluster: %v", err)
			log.Error(string(out))
			return nil, err
		}

		status := cr.Status
		status.Status = "pending"
		status.Message = "Cluster is being created"
		return &status, nil
	} else {
		if aksCluster.Status == "Succeeded" {
			status := cr.Status
			status.Status = "ready"
			status.Message = "Cluster ready"
			return &status, nil
		}
	}

	return &cr.Status, nil
}

func getRemoteCluster(name string, log *logrus.Entry) (*RemoteCluster, error) {
	cmd := exec.Command("az", "aks", "show", "--name ", name, "--resource-group ", name)
	out, err := cmd.Output()
	if err != nil {
		if strings.Contains(string(out), "not found") {
			return nil, nil
		} else {
			log.Errorf("Could not show clusters: %v", err)
			return nil, err
		}
	}

	var aksCluster RemoteCluster
	err = json.Unmarshal(out, &aksCluster)

	if err != nil {
		log.Errorf("Could not parse cluster list: %v", err)
		return nil, err
	}

	return &aksCluster, nil
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
