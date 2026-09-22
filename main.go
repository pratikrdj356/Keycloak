package main

import (
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	networkingv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/networking/v1"
	helmv3 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	apiextensions "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	"github.com/pulumi/pulumi-random/sdk/v4/go/random"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := loadConfig(ctx)

		// ---------------------------------------------------------------
		// 1. Namespace for Keycloak (isolated from the rest of the cluster)
		// ---------------------------------------------------------------
		ns, err := corev1.NewNamespace(ctx, "keycloak-ns", &corev1.NamespaceArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name: pulumi.String(cfg.Namespace),
				Labels: pulumi.StringMap{
					"app.kubernetes.io/part-of": pulumi.String("keycloak-local"),
				},
			},
		})
		if err != nil {
			return err
		}

		// ---------------------------------------------------------------
		// 2. cert-manager (issues the TLS certificate used by the ingress)
		// ---------------------------------------------------------------
		// Pulumi's Helm Chart resource does NOT auto-create the target
		// namespace the way `helm install --create-namespace` does, so it
		// has to exist before the chart's resources are applied.
		certManagerNs, err := corev1.NewNamespace(ctx, "cert-manager-ns", &corev1.NamespaceArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name: pulumi.String("cert-manager"),
			},
		})
		if err != nil {
			return err
		}

		certManager, err := helmv3.NewRelease(ctx, "cert-manager", &helmv3.ReleaseArgs{
			Chart:   pulumi.String("cert-manager"),
			Version: pulumi.String("v1.15.1"),
			RepositoryOpts: helmv3.RepositoryOptsArgs{
				Repo: pulumi.String("https://charts.jetstack.io"),
			},
			Namespace: pulumi.String("cert-manager"),
			Values: pulumi.Map{
				"installCRDs": pulumi.Bool(true),
			},
		}, pulumi.DependsOn([]pulumi.Resource{certManagerNs}))
		if err != nil {
			return err
		}

		// Self-signed ClusterIssuer. Swap for a Let's Encrypt / ACME issuer
		// if this is ever pointed at a publicly reachable host.
		issuer, err := apiextensions.NewCustomResource(ctx, "selfsigned-issuer", &apiextensions.CustomResourceArgs{
			ApiVersion: pulumi.String("cert-manager.io/v1"),
			Kind:       pulumi.String("ClusterIssuer"),
			Metadata: &metav1.ObjectMetaArgs{
				Name: pulumi.String("selfsigned-issuer"),
			},
			OtherFields: map[string]interface{}{
				"spec": map[string]interface{}{
					"selfSigned": map[string]interface{}{},
				},
			},
		}, pulumi.DependsOn([]pulumi.Resource{certManager}))
		if err != nil {
			return err
		}

		// ---------------------------------------------------------------
		// 3. Admin credentials — generated, never hard-coded
		// ---------------------------------------------------------------
		adminPassword, err := random.NewRandomPassword(ctx, "keycloak-admin-password", &random.RandomPasswordArgs{
			Length:  pulumi.Int(24),
			Special: pulumi.Bool(true),
			MinUpper: pulumi.Int(2),
			MinLower: pulumi.Int(2),
			MinNumeric: pulumi.Int(2),
			MinSpecial: pulumi.Int(2),
		})
		if err != nil {
			return err
		}

		adminSecret, err := corev1.NewSecret(ctx, "keycloak-admin-secret", &corev1.SecretArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String("keycloak-admin"),
				Namespace: ns.Metadata.Name(),
			},
			StringData: pulumi.StringMap{
				"admin-user":     pulumi.String("admin"),
				"admin-password": adminPassword.Result,
			},
		})
		if err != nil {
			return err
		}

		// ---------------------------------------------------------------
		// 4. Keycloak itself (Bitnami chart, HA-capable Quarkus distribution)
		// ---------------------------------------------------------------
		keycloak, err := helmv3.NewRelease(ctx, "keycloak", &helmv3.ReleaseArgs{
			Chart:   pulumi.String("keycloak"),
			Version: pulumi.String("22.1.0"),
			RepositoryOpts: helmv3.RepositoryOptsArgs{
				Repo: pulumi.String("https://charts.bitnami.com/bitnami"),
			},
			Namespace: ns.Metadata.Name().Elem(),
			Timeout:   pulumi.Int(600), // Keycloak + bundled Postgres pulling fresh images can take longer than Helm's 300s default
			Values: pulumi.Map{
				"global": pulumi.Map{
					"security": pulumi.Map{
						"allowInsecureImages": pulumi.Bool(true), // required since chart v16.3+ when pointing off the default bitnami/* registry
					},
				},
				"image": pulumi.Map{
					"repository": pulumi.String("bitnamilegacy/keycloak"), // bitnami/* tags were moved to bitnamilegacy/* in Aug 2025's catalog restructuring
				},
				"auth": pulumi.Map{
					"adminUser":         pulumi.String("admin"),
					"existingSecret":    pulumi.String("keycloak-admin"),
					"passwordSecretKey": pulumi.String("admin-password"),
				},
				"httpRelativePath": pulumi.String("/"),
				"proxy":            pulumi.String("edge"), // TLS terminates at the ingress
				"service": pulumi.Map{
					"type": pulumi.String("ClusterIP"),
				},
				"ingress": pulumi.Map{
					"enabled":   pulumi.Bool(true),
					"ingressClassName": pulumi.String("nginx"),
					"hostname":  pulumi.String(cfg.Hostname),
					"annotations": pulumi.Map{
						"cert-manager.io/cluster-issuer": pulumi.String("selfsigned-issuer"),
						"nginx.ingress.kubernetes.io/ssl-redirect": pulumi.String("true"),
					},
					"tls": pulumi.Bool(true),
					"extraTls": pulumi.Array{
						pulumi.Map{
							"hosts":      pulumi.StringArray{pulumi.String(cfg.Hostname)},
							"secretName": pulumi.String("keycloak-tls"),
						},
					},
				},
				"podSecurityContext": pulumi.Map{
					"enabled":     pulumi.Bool(true),
					"runAsNonRoot": pulumi.Bool(true),
					"runAsUser":   pulumi.Int(1000),
					"fsGroup":     pulumi.Int(1000),
				},
				"containerSecurityContext": pulumi.Map{
					"enabled":                pulumi.Bool(true),
					"readOnlyRootFilesystem": pulumi.Bool(false), // Keycloak writes to /tmp at runtime
					"allowPrivilegeEscalation": pulumi.Bool(false),
				},
				"resources": pulumi.Map{
					"requests": pulumi.Map{"cpu": pulumi.String("250m"), "memory": pulumi.String("512Mi")},
					"limits":   pulumi.Map{"cpu": pulumi.String("1"), "memory": pulumi.String("1Gi")},
				},
				"postgresql": pulumi.Map{
					"enabled": pulumi.Bool(true), // bundled DB is fine for a local/assignment cluster
					"image": pulumi.Map{
						"repository": pulumi.String("bitnamilegacy/postgresql"),
					},
					"volumePermissions": pulumi.Map{
						"image": pulumi.Map{
							"repository": pulumi.String("bitnamilegacy/os-shell"),
						},
					},
					"metrics": pulumi.Map{
						"image": pulumi.Map{
							"repository": pulumi.String("bitnamilegacy/postgres-exporter"),
						},
					},
					"auth": pulumi.Map{
						"postgresPassword": pulumi.String("keycloak"),
						"password":         pulumi.String("keycloak"),
					},
				},
			},
		}, pulumi.DependsOn([]pulumi.Resource{issuer, adminSecret}))
		if err != nil {
			return err
		}

		// ---------------------------------------------------------------
		// 5. Hardening: only the ingress controller may reach Keycloak,
		//    everything else in the namespace is default-deny.
		// ---------------------------------------------------------------
		_, err = networkingv1.NewNetworkPolicy(ctx, "keycloak-default-deny", &networkingv1.NetworkPolicyArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String("default-deny-ingress"),
				Namespace: ns.Metadata.Name(),
			},
			Spec: &networkingv1.NetworkPolicySpecArgs{
				PodSelector: &metav1.LabelSelectorArgs{},
				PolicyTypes: pulumi.StringArray{pulumi.String("Ingress")},
			},
		})
		if err != nil {
			return err
		}

		_, err = networkingv1.NewNetworkPolicy(ctx, "keycloak-allow-ingress-nginx", &networkingv1.NetworkPolicyArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String("allow-from-ingress-nginx"),
				Namespace: ns.Metadata.Name(),
			},
			Spec: &networkingv1.NetworkPolicySpecArgs{
				PodSelector: &metav1.LabelSelectorArgs{
					MatchLabels: pulumi.StringMap{
						"app.kubernetes.io/instance": pulumi.String("keycloak"),
					},
				},
				PolicyTypes: pulumi.StringArray{pulumi.String("Ingress")},
				Ingress: networkingv1.NetworkPolicyIngressRuleArray{
					&networkingv1.NetworkPolicyIngressRuleArgs{
						From: networkingv1.NetworkPolicyPeerArray{
							&networkingv1.NetworkPolicyPeerArgs{
								NamespaceSelector: &metav1.LabelSelectorArgs{
									MatchLabels: pulumi.StringMap{
										"kubernetes.io/metadata.name": pulumi.String("ingress-nginx"),
									},
								},
							},
						},
					},
				},
			},
		}, pulumi.DependsOn([]pulumi.Resource{keycloak}))
		if err != nil {
			return err
		}

		// ---------------------------------------------------------------
		// Outputs
		// ---------------------------------------------------------------
		ctx.Export("keycloakUrl", pulumi.Sprintf("https://%s", cfg.Hostname))
		ctx.Export("keycloakAdminUser", pulumi.String("admin"))
		ctx.Export("keycloakAdminPassword", pulumi.ToSecret(adminPassword.Result))
		ctx.Export("namespace", ns.Metadata.Name())

		return nil
	})
}

type stackConfig struct {
	Hostname  string
	Namespace string
}

func loadConfig(ctx *pulumi.Context) stackConfig {
	c := stackConfig{Hostname: "keycloak.local", Namespace: "keycloak"}
	cfg := config.New(ctx, "")
	if h := cfg.Get("hostname"); h != "" {
		c.Hostname = h
	}
	if n := cfg.Get("namespace"); n != "" {
		c.Namespace = n
	}
	return c
}
