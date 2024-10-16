package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	apicfgv1 "github.com/openshift/api/config/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	installertypes "github.com/openshift/installer/pkg/types"
	"github.com/openshift/installer/pkg/types/imagebased"
	"github.com/sirupsen/logrus"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/image-based-install-operator/api/v1alpha1"
	"github.com/openshift/image-based-install-operator/internal/credentials"
	"github.com/openshift/image-based-install-operator/internal/installer"
)

const validNMStateConfigBMH = `
interfaces:
  - name: enp1s0
    type: ethernet
    state: up
    identifier: mac-address
    mac-address: 52:54:00:8A:88:A8
    ipv4:
      address:
        - ip: 192.168.136.138
          prefix-length: "24"
      enabled: true
    ipv6:
      enabled: false
routes:
  config:
    - destination: 0.0.0.0/0
      next-hop-address: 192.168.136.1
      next-hop-interface: enp1s0
      table-id: 254
dns-resolver:
  config:
    server:
      - 192.168.136.1
`

const kubeconfig = `
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURoVENDQW0yZ0F3SUJBZ0lJSERHdXlSaFJTSnd3RFFZSktvWklodmNOQVFFTEJRQXdKakVrTUNJR0ExVUUKQXd3YmFXNW5jbVZ6Y3kxdmNHVnlZWFJ2Y2tBeE56QXpORE0wTmpFMU1CNFhEVEl6TVRJeU5ERTJNVFkxTlZvWApEVEkxTVRJeU16RTJNVFkxTmxvd01qRXdNQzRHQTFVRUF3d25LaTVoY0hCekxuTnVieTEzYjNKclpYSXRNREl1ClpUSmxMbUp2Y3k1eVpXUm9ZWFF1WTI5dE1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0NBUThBTUlJQkNnS0MKQVFFQXIyeXRhVTRCcS9GQzlMWGhYamIyV3BSMDUwRFgvZ3lMb0hlV1NERGp4bVJ4ZGFSaklzd0xFemVQYmk3dQo0R1A4cWNwL1RwNmlkaWRWOU8wRk40Q0htaWc5N1ZZcERycUVERDJZbmtBblBweHNEaWptRnFPNWJlVWVHMnZSCnVCbHFqVEp4VHowZ0JSbXQ4REMzNW5ib2ZsbURVemJkeHJtb2Ryd0RvMWl3U00yV1g2bnhxc2RZRDhwQkdNN1QKYnJIR3BKb2hub25WTVh3U2FzbTd6UExjQWloV2swWFpVcll5R25Xby91L2I1bmxUcGlGQkVHT1pPYTRiRUMxRwpUR0ljRHdVZ054WGUrUmpLWjNNU09ENDBHeHhHUlRua3llTldPYk9kbGlvOGxob2syMHc3OTZnb0FXZ3MreUFHCi9HbDNrRjFkdE9VdmpzTkJnQ0RCWWVDSVpRSURBUUFCbzRHcU1JR25NQTRHQTFVZER3RUIvd1FFQXdJRm9EQVQKQmdOVkhTVUVEREFLQmdnckJnRUZCUWNEQVRBTUJnTlZIUk1CQWY4RUFqQUFNQjBHQTFVZERnUVdCQlE5Z3FqQQpGOHExcEsrYVRrbVJkTGZGZTR5NmxUQWZCZ05WSFNNRUdEQVdnQlNpc2tXZGk4NFY1aHJxV0UzamE5MmR0dHpwCmJ6QXlCZ05WSFJFRUt6QXBnaWNxTG1Gd2NITXVjMjV2TFhkdmNtdGxjaTB3TWk1bE1tVXVZbTl6TG5KbFpHaGgKZEM1amIyMHdEUVlKS29aSWh2Y05BUUVMQlFBRGdnRUJBTHQrNnVER21VVm8wL2JnazJOeEw1ams3a2RLZWpicwovS3dlZmJUUTNRNXd2TzIvN0ZvZktZK2pwZWlUZ2g3ZkdoNXhCTzY5dW9PcjUvTXp0OGRaNU9JNlpnaHpOSzhOCmhWZ2R6VU9MUTN0YWhRcDdrUnpCOUhXdHM4TXVkeHJRU3JOWktGSVQwQnVhUlZER1JkdU1LMWNxOGNjVERub2EKQ0l5K0NiUjVGejJOd2I1TkNwVGxQN2RiOUhZUDdqNm1paG1aTm4zQkNHQjVkMEl3Qzh0VkVsbm1Yb0VLVlRjTgpSQks5cnkxdzkyKy9sWWNYcHdqQ2g1bEFRWURHSXhmSS9UYkc2WElrQ3EwK3YvZGdJdGpCelExdDlFNmRIOStlCkpaU2Q4enVUSFp6YzZod1VtcFlOMEIwb09hb3VlbXRheXV3eldhek5yVzRjTUsvcHpmdFNhMTQ9Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0KLS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURERENDQWZTZ0F3SUJBZ0lCQVRBTkJna3Foa2lHOXcwQkFRc0ZBREFtTVNRd0lnWURWUVFEREJ0cGJtZHkKWlhOekxXOXdaWEpoZEc5eVFERTNNRE0wTXpRMk1UVXdIaGNOTWpNeE1qSTBNVFl4TmpVMVdoY05NalV4TWpJegpNVFl4TmpVMldqQW1NU1F3SWdZRFZRUUREQnRwYm1keVpYTnpMVzl3WlhKaGRHOXlRREUzTURNME16UTJNVFV3CmdnRWlNQTBHQ1NxR1NJYjNEUUVCQVFVQUE0SUJEd0F3Z2dFS0FvSUJBUURNVHdrSnZJR2Y0MTZWeHRKdHFEc1MKWExjanIvWGdrNHVwbmNvSVVZeDBPWlE2ZmgxTkhuQWVBelVacUZIdzRxcXVVZThQOG05bFE2eWxyVDNYUFpKTgpoM0QrZnM1RXYrdGM1V29qY2trMlRxbVZDR3ZRb3lWUTg4eDUrK3lHNU1KRkNCazJ0U3VSVGxDMmUyZDMraHVaCmdlUXN6NGpsWHpzQ3pjMENNVC9QUFhiSm1Jd3FGc2JPL1NFdk5XeEd5Nk9zVXgybkRvTjlPNGt0cmdjT1lPTHgKMzdob2pMZzVpam1OYWltS3FzRldGdVBUVklsbkFVVnVpTmJqVFVtQVlzZXJtaGt6UlBBLzFnQVJ1bmU2MkVEMgpid094VSt2QndQSlZkcDF2SXhhdnNLNENSM3lsWTZ5dFl5SWZoTVBzcmREZHViQUdTYURmZ293dEtpWXBnS2k1CkFnTUJBQUdqUlRCRE1BNEdBMVVkRHdFQi93UUVBd0lDcERBU0JnTlZIUk1CQWY4RUNEQUdBUUgvQWdFQU1CMEcKQTFVZERnUVdCQlNpc2tXZGk4NFY1aHJxV0UzamE5MmR0dHpwYnpBTkJna3Foa2lHOXcwQkFRc0ZBQU9DQVFFQQpDc0tYR0dsZnJlSDIwNFByZFVIb1p3WVBMemdEZXpXR0h3QVJhSHprS0RjdXJXRnlaNmROYnJ3cUdzalljVXA1Cjk3TG83ZWZQRjZHa0pIamg1cUdyRThQR1pmeEhmc3MvVGE1VmJmamJzSTcxTCtiRDduNC8rL2N2Y1RnczFsR3UKUFN5VktOako3a1dVdjJ5Tk9kSG9KNWJTRXBxSWpzOTBRMW5wQVRkRklYVkthd25xNWxVUVBjYmZ4SmxlTm5ILwp2RU10QzFkb1c3MWdONTFBampOYzV5T3VvZ3RwNTZ0MXkzWGlLa3NJek9hSXFwbzBkdlpvS1Mwa2ZjK01rWk1JCnhmVnY3OVBjeW42N1IvTC8yajRpU1labkhvVU11L1dTN2tieFBoem53dTRnZnRLbjVzazJtVnJhRzBKWlNRcXQKakVJandNYVdUL3V1azZmNEl0OXVCQT09Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0KLS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURRRENDQWlpZ0F3SUJBZ0lJUkkzWWNvazJ0cGN3RFFZSktvWklodmNOQVFFTEJRQXdQakVTTUJBR0ExVUUKQ3hNSmIzQmxibk5vYVdaME1TZ3dKZ1lEVlFRREV4OXJkV0psTFdGd2FYTmxjblpsY2kxc2IyTmhiR2h2YzNRdApjMmxuYm1WeU1CNFhEVEl6TVRJeU5ERTFOVGt4TVZvWERUTXpNVEl5TVRFMU5Ua3hNVm93UGpFU01CQUdBMVVFCkN4TUpiM0JsYm5Ob2FXWjBNU2d3SmdZRFZRUURFeDlyZFdKbExXRndhWE5sY25abGNpMXNiMk5oYkdodmMzUXQKYzJsbmJtVnlNSUlCSWpBTkJna3Foa2lHOXcwQkFRRUZBQU9DQVE4QU1JSUJDZ0tDQVFFQXVaUWorcE9DOFIyawpCZ3VVMWhNdTlYektMSEJqQVlDY1BOTDhaRGhxWFlUbkZhc0FZSzdaOWVvN09uQ24xUWlZeTY3U0oybGVIVDNOCnY5U0RpWVdSN2tIZzAvMlVPUzVzd055bHFjQ01iWVBaT2xsTGUyamdkTnA1MWNmSkZkZTFmTWY0VjRZOExwcE8KMmZSZDZETERCUDRlUjBwNkJlMHdWcnV6SW1yYVg5K291aHdXQWkzdU1BcC9WcEIvanBEYnUzVnFTZ3dkN1g2VQpmZ281aXhNS1h3dWQ0UGN0d3FEa01vVXQyL2NMT1dZYUZYdWFZWXJrMWM4UVhMMkg3RVhucHp6TVJiMnlMRlpECnhWWm9URDJISXJJNjQvNzFjT3daYmNYNGhoUFZUQnRuNlR2KzBpc1FTNldHZWlNRC9jcHhUdG0vYm9KL0JjNXUKSHRGNkVoMGx6UUlEQVFBQm8wSXdRREFPQmdOVkhROEJBZjhFQkFNQ0FxUXdEd1lEVlIwVEFRSC9CQVV3QXdFQgovekFkQmdOVkhRNEVGZ1FVSzUvMDFlSUhnWTZHa3Z0Uy8rVWdEWlpQdURZd0RRWUpLb1pJaHZjTkFRRUxCUUFECmdnRUJBSzBHZC9qZFhIY3NLdGdUQUJEK1FVVkw3T1hoZEcxQXRjRlExTEdyczhTMzFXYjZKWVV2eW1tTEQxdnMKaHhmSzdDWFdBK1dCbU1FdVl5Y0llNlRBTjljelU3am8yU2Y5amY1VkdqTXUrc2RFQ2RrNnBicHhoTnRvcmY3cAp5YjRTYUptM3prZFZjNnpSeEs3ZkFqZGFDT1Y2M05VVkRnYXRmZDY0aXJoZnVuSGw3OHY5T2tqUUlwSnlZMWJKCnBYdVM3d0plczBsTUdlM3BpODZBaFFDV0FRV0FPSmRFQ293bUtCaXJ6NWJIT0gvb3BtMWlJS251SXBrQi9PTzIKdk1GWDN6VWw0cHpmZmlUVzhxTEZMc1VkRXRUSmlQQnpTZmVOaGYxSmowcXVnZ2dIMDFCM2xQQldNRHAyaUQ5MAp1R0dCdkhnYXBOSHlPWXlJdkJhRFpOcEpuTzA9Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0KLS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURURENDQWpTZ0F3SUJBZ0lJZlRMK2g1dmhFaU13RFFZSktvWklodmNOQVFFTEJRQXdSREVTTUJBR0ExVUUKQ3hNSmIzQmxibk5vYVdaME1TNHdMQVlEVlFRREV5VnJkV0psTFdGd2FYTmxjblpsY2kxelpYSjJhV05sTFc1bApkSGR2Y21zdGMybG5ibVZ5TUI0WERUSXpNVEl5TkRFMU5Ua3hNVm9YRFRNek1USXlNVEUxTlRreE1Wb3dSREVTCk1CQUdBMVVFQ3hNSmIzQmxibk5vYVdaME1TNHdMQVlEVlFRREV5VnJkV0psTFdGd2FYTmxjblpsY2kxelpYSjIKYVdObExXNWxkSGR2Y21zdGMybG5ibVZ5TUlJQklqQU5CZ2txaGtpRzl3MEJBUUVGQUFPQ0FROEFNSUlCQ2dLQwpBUUVBdHZHTkZOSGJzUy9JUThZbDZJK1QvMVFnZFpBVlQ0dFdnY2VUWkNvQ09kM3dmTWJtdWx4a3dReHV2c0g4CmxYNUZJWDFtTHh2aStoeGNQQ1pCb0Rrdk85MEhmVlVJdXRCSHNCa1NCbytRRmZ0OG1MUS9wWjlvd3dyZGQ5RHgKSTV4V2EvcXV6c3I1eFNGV2tlVyt4cXY3cXJlUXJyYytzcEhvWnNXYjN0c1RBbm1jRU1HSTY4NEUzeXp6VlJwRgoyUlZrdHR0OXBIL1VDOGgvMGZIdnA0SjNBOWhPMmhVdXh6Zk5Id3ZoeEQwcFIrMkovdU05ODdRQ29BWUhiZTJECmJCMHBCb3F4MVBhNERnQnRJeUh2RnREQW9VcHhsTW0wemFrMUYzWjBYalUvUmE5Rkhta0IzK2lNWjFNK051cTkKTjFOcURQbXBBWXhqeFJuLzA1VUUwcW9BalFJREFRQUJvMEl3UURBT0JnTlZIUThCQWY4RUJBTUNBcVF3RHdZRApWUjBUQVFIL0JBVXdBd0VCL3pBZEJnTlZIUTRFRmdRVVovR1cvaXlabXJLcmpEZGhwK29ZWDFqdFdac3dEUVlKCktvWklodmNOQVFFTEJRQURnZ0VCQUlMeXRTQXBJL21HU1FkY2xqQkVlVVdramVCSC9IbEIwNGpOWDFObk50RlkKdmxsNHdyZEFVb0NibTdiSDgvZ0x0NjdGQ0g2djhvZnlKRUZacEZkaHRyZUg5Y1NFRjNYbnh1c0V2WkhzMFp5SQpVOTZiL3lLSkJKUTVLMlY0VUR5L0JXai9FQmtBbG95c3NlYXQ1cnBaK2x0dEFRQmIyc05SSDVTd2lxcXZNQmx0ClBZV2Q2dnJ4dytxbTBLVjc3RDZ3OVpKL2hZQWhuY0dZVDhHUXEvTVBVN2VhdzFLYk5obXltSmRZSlc3YS9uYWYKUno4Y3phcmtnV3Bkdks4WjJsMWlaNzFlbjRwaUpkb0NXbjdsTndGa1RxdXJOMjNZemhDbE8walBuNFBEaG90RQpkUUNmUUJ2RWVITlRKcUJzZjk3Yy9kblF1ci9oZC9TRWxUZnIvTGJ0UjI0PQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCi0tLS0tQkVHSU4gQ0VSVElGSUNBVEUtLS0tLQpNSUlETWpDQ0FocWdBd0lCQWdJSUp5Y1BnZ3BzZ2F3d0RRWUpLb1pJaHZjTkFRRUxCUUF3TnpFU01CQUdBMVVFCkN4TUpiM0JsYm5Ob2FXWjBNU0V3SHdZRFZRUURFeGhyZFdKbExXRndhWE5sY25abGNpMXNZaTF6YVdkdVpYSXcKSGhjTk1qTXhNakkwTVRVMU9URXhXaGNOTXpNeE1qSXhNVFUxT1RFeFdqQTNNUkl3RUFZRFZRUUxFd2x2Y0dWdQpjMmhwWm5ReElUQWZCZ05WQkFNVEdHdDFZbVV0WVhCcGMyVnlkbVZ5TFd4aUxYTnBaMjVsY2pDQ0FTSXdEUVlKCktvWklodmNOQVFFQkJRQURnZ0VQQURDQ0FRb0NnZ0VCQUtnYm0rTVVZQ3JyODRhamJxaUVRM0x4WUs3VGd6cnIKUklLYzFKSXhlRlJiOTF4NmVxYTZ2cHFHTS94cXFESjVoMjc5c2lyMmdWSm9FYXZRYTV3N0RFZkE3UGFhSEFPMgpNMUIzTnF0VjVuRDJTSWZKdDRtYmhsNjd5K1NOelRiSjRTZ2dmdW9IUkRpL2VaOTlDVU1qdi9mNkh1V3dZWDAwCm1HYWlhZGZqQjJmWHRuSGdrbFEwQ0NRVXVXWHhoS1dsN3NpMVBUWU55Tll5RGR3ZXlmeEZ5bkJzaUVtNkwzZGYKd3RQYVRHQkI1V0xqK2ZLckEvZjFFQkFobEFIdUFXNDh6Rmk1L1ZGcnB1ais3eWVLMnhYTHBhS1VSTnpZektmSwppd1U2R3hmNjEyN0hycU16dVRzYXlPWG5FWExIenhDMGFQcEV2dU4xU04vMDBlcE1WRDNoL2k4Q0F3RUFBYU5DCk1FQXdEZ1lEVlIwUEFRSC9CQVFEQWdLa01BOEdBMVVkRXdFQi93UUZNQU1CQWY4d0hRWURWUjBPQkJZRUZOWFkKOGF0aCt0MFZDeXlOcTNOVlZVbnRRUDAvTUEwR0NTcUdTSWIzRFFFQkN3VUFBNElCQVFCOGQzZDZIOXNaZkdLLwpGbDMwVk93V0tJQ2c1YUZCSlQwSFRvVzZsVllVSDNjblNsYlhtM1p6Y3JVWkFWOWFRYWNDc0JkUkFuR2hwa2hBClVva2lHZUMxWkpZWVV4Y0t0YlF6Zi85S05HT2V3MGVJb0tPYzZxaWoyVm94aGN2YUtaVVplVEsvZjd0bURaVDIKQTc3Sm8wRUVFMTFIZUVudW1SdmFab0JTc2g4Yy9DMlFnejBuSVYreGttS2o3MW9iLzQzaWt6aGxMYzRaSjBONwpzSHROTTdSSUIwSUY5aU1mNGZLMklSRFg5WlBoQkFLUFdRTHFhQzBVRDZZVFZHQnNzR081S2pzSUJKbExhT3cyCldJYTFHd2wxbVN4WGlrTEpLdEd0bmVFRmhRaVVnenl5WWg4RjA0a252SVhISFBNd01EUHB2SVdCNGRGVERCRVMKVXlsY2ZRN3QKLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo=
    server: https://api.sno-worker-02.e2e.bos.redhat.com:6443
  name: sno-worker-02
contexts:
- context:
    cluster: sno-worker-02
    user: admin
  name: admin
current-context: admin
kind: Config
preferences: {}
users:
- name: admin
  user:
    client-certificate-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURaekNDQWsrZ0F3SUJBZ0lJUll4dDdScGdkdm93RFFZSktvWklodmNOQVFFTEJRQXdOakVTTUJBR0ExVUUKQ3hNSmIzQmxibk5vYVdaME1TQXdIZ1lEVlFRREV4ZGhaRzFwYmkxcmRXSmxZMjl1Wm1sbkxYTnBaMjVsY2pBZQpGdzB5TXpFeU1qUXhOVFU1TVRCYUZ3MHpNekV5TWpFeE5UVTVNVEJhTURBeEZ6QVZCZ05WQkFvVERuTjVjM1JsCmJUcHRZWE4wWlhKek1SVXdFd1lEVlFRREV3eHplWE4wWlcwNllXUnRhVzR3Z2dFaU1BMEdDU3FHU0liM0RRRUIKQVFVQUE0SUJEd0F3Z2dFS0FvSUJBUUQzSnUwR3FPcVhJR1piMzl0MVlqa2xCVXBGL3cvZzlrUHNpazNxb242ZQpnNDZrcWFkd09xQm1lc25NSjRrQVNpTWltY1RnaHJhZmY5cVdsdVp4NWdYMEcvbDY4YlBPUmdJM2I1T2Z4dG1YCitHUmhHOEFLMFVjNXRESU01WXVyb2gwUVJoZmtIcGZXb1IvNDJnQmtpNkYwNDFnYzJ4a2dDNjNPNTdzODZPVDkKWHRieXMxWXdEMVA5MkNxNXBPSkZyNUYrTUIydDJPMGs2N2xMaTlLQkZleU83TmE4eHdKci8waTkwTEdvSE42YQpKWGMvSSt0Zmo0VEtJM3J5aTlMbktjQmY1Wk0yWC94cmlmWWdOR0d4YVlxTWY0Z2FhaTN3Z2hPWm5KWC9INlpYCktaZUk2aCszMmhBT3JwYTE5OVRDWlFtcEF6YlV3eSsrSThxS2FocHd0UWVqQWdNQkFBR2pmekI5TUE0R0ExVWQKRHdFQi93UUVBd0lGb0RBZEJnTlZIU1VFRmpBVUJnZ3JCZ0VGQlFjREFRWUlLd1lCQlFVSEF3SXdEQVlEVlIwVApBUUgvQkFJd0FEQWRCZ05WSFE0RUZnUVUyeEw2am9ibzl2MldETjMzV0dhUXJRU01zVzh3SHdZRFZSMGpCQmd3CkZvQVVlTXdFWEIycGVONUN0NmZvdndHVmJTeklhOUV3RFFZSktvWklodmNOQVFFTEJRQURnZ0VCQUN5Sy9uUzMKNWo1eWw3eU02Q2t6NE4wUnJpRlJ1enRqOXVMdHYwWFZnelRCdXV3ZFNrMFI5UXc2Y0lqdm91TlFwRm1lR3ZCWQp6M1FOTkhTWldSSFVHZTFDRU1rNFI4bDNKVWxBV3N6Umc2V2ZpSmNGTHUxMSs0cUx0cUtRQVo0WU9aZ3dSTExSClFoc2VjOG9Ha3doT0FiTWRCWkgvS0R2ZUFuMitkd3JGM0E4OVRlTy9WN2hCUi9FMWdUazRidmxWbEJtN1ZWVFQKQmFnV3gzLzQxem1sMHRXNzUrSnc0d0lGZFMrZ05za29JOUFhWEZ6QnRLK1BoMi9lbWllSUo3NDM1TGJCa0ZQdQp4cnZ3TUxPUFFGbG5tR2U4TzBKSmh4emxUbzNROEJUdUhzVjFPK0hBbDdETFNtY1VhMFVOSStDaWxpUVllODdGCjdkQlZ5OU9zTHpRY3RwWT0KLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo=
    client-key-data: LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlFcEFJQkFBS0NBUUVBOXlidEJxanFseUJtVzkvYmRXSTVKUVZLUmY4UDRQWkQ3SXBONnFKK25vT09wS21uCmNEcWdabnJKekNlSkFFb2pJcG5FNElhMm4zL2FscGJtY2VZRjlCdjVldkd6emtZQ04yK1RuOGJabC9oa1lSdkEKQ3RGSE9iUXlET1dMcTZJZEVFWVg1QjZYMXFFZitOb0FaSXVoZE9OWUhOc1pJQXV0enVlN1BPamsvVjdXOHJOVwpNQTlUL2RncXVhVGlSYStSZmpBZHJkanRKT3U1UzR2U2dSWHNqdXpXdk1jQ2EvOUl2ZEN4cUJ6ZW1pVjNQeVByClg0K0V5aU42OG92UzV5bkFYK1dUTmwvOGE0bjJJRFJoc1dtS2pIK0lHbW90OElJVG1aeVYveCttVnltWGlPb2YKdDlvUURxNld0ZmZVd21VSnFRTTIxTU12dmlQS2ltb2FjTFVIb3dJREFRQUJBb0lCQUVyQ2k1QW9LRTN1andmYgpmeGJTejFaVGMxUVpBMFNaT1pLamcwNG1PUWJaNUp3S2RZdU5NRmZQYkp0RW1qeHNNSlNXenViYjJRSUdPcWl5Cm5LSjNZZldsUUtIZjJ2UGFXWEZMWHV4Rnlpd2VCcjhaRmM0djM4dWtwajhnY0U5S2ltQVIwOGc5T05ERGpGaEsKR1RSUXlGWURMdlFMa2w0UEtsUWI1SmRZRzJ4SVc2ME40bHZudHNnRi9FeEdtdkRFem5kOXcxWUQvMEkrdUNOSAorU3R2eWcxbGJPN3lzcUJOMWtRaGZxck93eGowTnAwQndKbmtlOE80LzE4cGJBZkVIVjVQUVhzMFg3UWw5YmdpCjJrMWUxVzFJMm1wZ1NqOTIrMGljVmJpSys5Ukh3NVRYbzVwUWljK2pXWHlYV3VMcStRdEtYOUsvYmdmVms2MkgKdGZtV1FBRUNnWUVBK0lnUHZQRm44dkt1VWJYU0VLRHBXR0lOODRlZVlGMVJJR1laZVphV1NCZXVEL0xhdnVUcwphcnNuQThneW92SXNwTlRCNlRPUDE4OUVBdHhCNW40SmFaQ0doRW5MUjMweDdnMmx5S0dtL0VzbEd4QjNtWXowCmxvZDlidTdxRmNoSlVETEVtMVBlekVXT2VaWURvcFhnejJrWWtsZEdlbWY1cWwrNnNSV0JaYU1DZ1lFQS9wUkEKb3RSU2JKNGNYSm4vV3ZNVi9OTSs4NDJtVmliaGlqTlhIcHNLb0NuRkpzOU11L0tDV3pJVnRGUGNhcXBiWW1tbgpxM0xObWQxNHZXZktGc0NHSXJZQU9TVlRSa2U4aHZYdTJicnRzK2p4WFV0T3EvSlBYVUhBaXdEcER6bC9FUW9zCk9mdVY5L1JZWnhoTVUwNklhWHYva3ZiOUFoclJpMXlLTE16ajlnRUNnWUVBc1lpSUxZRUlyeng2RjNaWlEzV2UKbW5zWTJqQ3ppc1MrTWZXZEZ3VGgvMGVqR0ZOenZNOU1yb1ZZUGxSaEQ1TU5Id1c0S3Z5aE1Sa216U0JIbHYzTApmWFEzRXdLZnlrWjlSejdLZ1VJd1JhTHNNNlFVdTBROFo5Y0xYQllnYzBUcVVmYWlDUTdsWldmK2VZNFd0S3A1CnQ4K3NOTVZSWEJrditHdC9zZ1hadEI4Q2dZQVg5cXFTNlR1TS8rRVprbUZvRlVPM2xjYnlOQjQ1TTlXOUpaSUkKem4xVWtEbi9xam5GNDFFRDlwWDJjSUpxQS9rd0xWUGNIcVZkMjJ3WElDTDB1MUNsQ2M3QmtsTGhaYlZJV3ZRTgp5THZCV0tjSHFpUVFxWEZ4RE5Sc0FUenU4dkdVRUFvVHR5dnB1RFZ1RnVwd1dROGNKdERxNjViclVNenl1bFpEClcxSUdBUUtCZ1FEUlhPOFJVNzJVVU5JTDk4ZWZST3VmV2M3VG1BR0VwTlpMbEdRSjdtdEx5dFZPOUluRzlYeFQKOWRpQXZ5ZGZzMHBLRTlwVVRKbGJ1N1pBV1JmZUZiMWxyMHFCVkVzeWVmWkdSUWVwTUZ3RnprUVhZbEIrRXhBbQpITmxkQ2dLaU1Vby9OUmZyU0JRZ1BhTzl0YWFZY2xzWFRzTnUrOGdxS1lHQnJpMlpSTVdGUmc9PQotLS0tLUVORCBSU0EgUFJJVkFURSBLRVktLS0tLQo=
`

func bmhInState(state bmh_v1alpha1.ProvisioningState) *bmh_v1alpha1.BareMetalHost {
	return &bmh_v1alpha1.BareMetalHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-bmh",
			Namespace: "test-bmh-namespace",
		},
		Status: bmh_v1alpha1.BareMetalHostStatus{
			Provisioning: bmh_v1alpha1.ProvisionStatus{
				State: state,
			},
			HardwareDetails: &bmh_v1alpha1.HardwareDetails{NIC: []bmh_v1alpha1.NIC{{IP: "1.1.1.1"}}},
			PoweredOn:       state == bmh_v1alpha1.StateExternallyProvisioned,
		},
		Spec: bmh_v1alpha1.BareMetalHostSpec{
			ExternallyProvisioned: state == bmh_v1alpha1.StateExternallyProvisioned,
		},
	}
}

type FakeClientWithTimestamp struct {
	client.Client
}

func (c FakeClientWithTimestamp) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	// Check if the object is of type DataImage
	if dataImage, ok := obj.(*bmh_v1alpha1.DataImage); ok {
		// Log the creation of a DataImage object
		fmt.Printf("Creating DataImage object: %+v\n", dataImage)
		// If the CreationTimestamp is not set, set it to now
		if dataImage.GetCreationTimestamp().Time.IsZero() {
			fmt.Println("Setting CreationTimestamp for DataImage.")
			dataImage.SetCreationTimestamp(metav1.Now())
		}
	}
	return c.Client.Create(ctx, obj, opts...)
}

var _ = Describe("Reconcile", func() {
	var (
		c                       client.Client
		mockCtrl                *gomock.Controller
		dataDir                 string
		r                       *ImageClusterInstallReconciler
		ctx                     = context.Background()
		clusterInstallName      = "test-cluster"
		clusterInstallNamespace = "test-namespace"
		clusterInstall          *v1alpha1.ImageClusterInstall
		clusterDeployment       *hivev1.ClusterDeployment
		pullSecret              *corev1.Secret
		installerMock           *installer.MockInstaller
		testPullSecretVal       = `{"auths":{"cloud.openshift.com":{"auth":"dXNlcjpwYXNzd29yZAo=","email":"r@r.com"}}}`
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		fc := fakeclient.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&v1alpha1.ImageClusterInstall{}).
			Build()
		c = FakeClientWithTimestamp{Client: fc}
		var err error
		dataDir, err = os.MkdirTemp("", "imageclusterinstall_controller_test_data")
		Expect(err).NotTo(HaveOccurred())
		cm := credentials.Credentials{
			Client: c,
			Log:    logrus.New(),
			Scheme: scheme.Scheme,
		}

		installerMock = installer.NewMockInstaller(mockCtrl)
		r = &ImageClusterInstallReconciler{
			Client:      c,
			Credentials: cm,
			Scheme:      scheme.Scheme,
			Log:         logrus.New(),
			BaseURL:     "https://images-namespace.cluster.example.com",
			Options: &ImageClusterInstallReconcilerOptions{
				DataDir:                 dataDir,
				DataImageCoolDownPeriod: time.Duration(0),
			},
			NoncachedClient: c,
			Installer:       installerMock,
		}

		imageSet := &hivev1.ClusterImageSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "imageset",
				Namespace: clusterInstallNamespace,
			},
			Spec: hivev1.ClusterImageSetSpec{
				ReleaseImage: "registry.example.com/releases/ocp@sha256:0ec9d715c717b2a592d07dd83860013613529fae69bc9eecb4b2d4ace679f6f3",
			},
		}
		Expect(c.Create(ctx, imageSet)).To(Succeed())
		pullSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ps",
				Namespace: clusterInstallNamespace,
			},
			Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(testPullSecretVal)},
		}
		Expect(c.Create(ctx, pullSecret)).To(Succeed())

		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-1",
				Namespace: "test-bmh-namespace",
			},
			Status: bmh_v1alpha1.BareMetalHostStatus{
				Provisioning: bmh_v1alpha1.ProvisionStatus{
					State: bmh_v1alpha1.StateExternallyProvisioned,
				},
				HardwareDetails: &bmh_v1alpha1.HardwareDetails{NIC: []bmh_v1alpha1.NIC{{IP: "1.1.1.1"}}},
				PoweredOn:       true,
			},
			Spec: bmh_v1alpha1.BareMetalHostSpec{
				ExternallyProvisioned: true,
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall = &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
				UID:        types.UID(uuid.New().String()),
			},
			Spec: v1alpha1.ImageClusterInstallSpec{
				ImageSetRef: hivev1.ClusterImageSetReference{
					Name: imageSet.Name,
				},
				ClusterDeploymentRef: &corev1.LocalObjectReference{Name: clusterInstallName},
				BareMetalHostRef: &v1alpha1.BareMetalHostReference{
					Name:      bmh.Name,
					Namespace: bmh.Namespace,
				},
			},
		}

		clusterDeployment = &hivev1.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterInstallName,
				Namespace: clusterInstallNamespace,
			},
			Spec: hivev1.ClusterDeploymentSpec{
				ClusterInstallRef: &hivev1.ClusterInstallLocalReference{
					Group:   clusterInstall.GroupVersionKind().Group,
					Version: clusterInstall.GroupVersionKind().Version,
					Kind:    clusterInstall.GroupVersionKind().Kind,
					Name:    clusterInstall.Name,
				},
				PullSecretRef: &corev1.LocalObjectReference{
					Name: "ps",
				},
			}}
	})

	AfterEach(func() {
		mockCtrl.Finish()
		Expect(os.RemoveAll(dataDir)).To(Succeed())
	})

	imageURL := func() string {
		return fmt.Sprintf("https://images-namespace.cluster.example.com/images/%s/%s.iso", clusterInstallNamespace, clusterInstall.ObjectMeta.UID)
	}

	outputFilePath := func(elem ...string) string {
		last := filepath.Join(elem...)
		return filepath.Join(dataDir, "namespaces", clusterInstallNamespace, string(clusterInstall.ObjectMeta.UID), "files", last)
	}

	installerSuccess := func() {
		installerMock.EXPECT().CreateInstallationIso(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil).Times(1).Do(func(any, any, any) {
			Expect(os.WriteFile(outputFilePath(ClusterConfigDir, IsoName), []byte("test"), 0644)).To(Succeed())
			Expect(os.MkdirAll(outputFilePath(ClusterConfigDir, authDir), 0700)).To(Succeed())
			Expect(os.WriteFile(outputFilePath(ClusterConfigDir, authDir, kubeAdminFile), []byte("test"), 0644)).To(Succeed())
			Expect(os.WriteFile(outputFilePath(ClusterConfigDir, authDir, credentials.Kubeconfig), []byte(kubeconfig), 0644)).To(Succeed())
		})
	}

	validateExtraManifestContent := func(file string, data string) {
		content, err := os.ReadFile(outputFilePath(ClusterConfigDir, extraManifestsDir, file))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal(data))
	}

	It("creates the correct cluster info manifest", func() {
		clusterInstall.Spec.MachineNetwork = "192.0.2.0/24"
		clusterInstall.Spec.Hostname = "thing"
		clusterInstall.Spec.SSHKey = "my ssh key"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		clusterDeployment.Spec.BaseDomain = "example.com"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(ClusterConfigDir, installConfigFilename))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &installertypes.InstallConfig{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())

		Expect(infoOut.BaseDomain).To(Equal(clusterDeployment.Spec.BaseDomain))
		Expect(infoOut.ObjectMeta.Name).To(Equal(clusterDeployment.Spec.ClusterName))

		Expect(infoOut.MachineNetwork[0].CIDR.String()).To(Equal(clusterInstall.Spec.MachineNetwork))
		Expect(infoOut.SSHKey).To(Equal(clusterInstall.Spec.SSHKey))
		Expect(infoOut.PullSecret).To(Equal(testPullSecretVal))

		content, err = os.ReadFile(outputFilePath(ClusterConfigDir, imageBasedConfigFilename))
		Expect(err).NotTo(HaveOccurred())
		config := &imagebased.Config{}
		Expect(json.Unmarshal(content, config)).To(Succeed())
		Expect(config.ClusterID).ToNot(Equal(""))
		Expect(config.InfraID).ToNot(Equal(""))
		Expect(config.ReleaseRegistry).To(Equal("registry.example.com"))
		Expect(config.Hostname).To(Equal(clusterInstall.Spec.Hostname))
	})

	It("installer command fails", func() {
		clusterInstall.Spec.MachineNetwork = "192.0.2.0/24"
		clusterInstall.Spec.Hostname = "thing"
		clusterInstall.Spec.SSHKey = "my ssh key"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		clusterDeployment.Spec.BaseDomain = "example.com"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		installerMock.EXPECT().CreateInstallationIso(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("failed")).Times(1)
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
	})

	It("pullSecret not set", func() {
		clusterDeployment.Spec.PullSecretRef = nil
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing reference to pull secret"))
		Expect(res).To(Equal(ctrl.Result{}))
	})

	It("missing pullSecret", func() {
		clusterDeployment.Spec.PullSecretRef.Name = "nonExistingPS"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to find secret"))
		Expect(res).To(Equal(ctrl.Result{}))
	})

	It("pullSecret missing dockerconfigjson key", func() {
		pullSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ps",
				Namespace: clusterInstallNamespace,
			},
			Data: map[string][]byte{},
		}
		Expect(c.Update(ctx, pullSecret)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("secret ps did not contain key .dockerconfigjson"))
		Expect(res).To(Equal(ctrl.Result{}))
	})

	It("malformed pullSecret", func() {
		pullSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ps",
				Namespace: clusterInstallNamespace,
			},
			Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte("garbage")},
		}
		Expect(c.Update(ctx, pullSecret)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid pull secret data in secret pull secret must be a well-formed JSON"))
		Expect(res).To(Equal(ctrl.Result{}))
	})

	It("creates the ca bundle", func() {
		caData := map[string]string{caBundleFileName: "mycabundle"}
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ca-bundle",
				Namespace: "test-namespace",
			},
			Data: caData,
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		clusterInstall.Spec.CABundleRef = &corev1.LocalObjectReference{
			Name: "ca-bundle",
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(ClusterConfigDir, installConfigFilename))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &installertypes.InstallConfig{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		Expect(infoOut.AdditionalTrustBundle).To(Equal("mycabundle"))

	})

	It("creates the invoker CM", func() {
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(ClusterConfigDir, extraManifestsDir, invokerCMFileName))
		Expect(err).NotTo(HaveOccurred())
		cm := corev1.ConfigMap{}
		err = json.Unmarshal(content, &cm)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data["invoker"]).To(Equal(imageBasedInstallInvoker))
	})

	It("creates the imageDigestMirrorSet", func() {
		imageDigestMirrors := []apicfgv1.ImageDigestMirrors{
			{
				Source: "registry.ci.openshift.org/ocp/release",
				Mirrors: []apicfgv1.ImageMirror{
					"virthost.ostest.test.metalkube.org:5000/localimages/local-release-image",
				},
			},
			{
				Source: "quay.io/openshift-release-dev/ocp-v4.0-art-dev",
				Mirrors: []apicfgv1.ImageMirror{
					"virthost.ostest.test.metalkube.org:5000/localimages/local-release-image",
				},
			},
		}
		clusterInstall.Spec.ImageDigestSources = imageDigestMirrors
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(ClusterConfigDir, installConfigFilename))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &installertypes.InstallConfig{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())

		Expect(len(infoOut.ImageDigestSources)).To(Equal(2))
		Expect(infoOut.ImageDigestSources).To(Equal(installer.ConvertIDMToIDS(imageDigestMirrors)))
		Expect(len(infoOut.ImageDigestSources[0].Mirrors)).To(Equal(1))

	})

	It("copies the nmstate config bmh preprovisioningNetworkDataName", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "netconfig",
				Namespace: bmh.Namespace,
			},
			Data: map[string][]byte{nmstateSecretKey: []byte(validNMStateConfigBMH)},
		}
		Expect(c.Create(ctx, secret)).To(Succeed())

		bmh.Spec.PreprovisioningNetworkDataName = "netconfig"
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(ClusterConfigDir, imageBasedConfigFilename))
		Expect(err).NotTo(HaveOccurred())
		config := &imagebased.Config{}
		Expect(json.Unmarshal(content, config)).To(Succeed())
		var jsonMap map[string]interface{}
		Expect(yaml.Unmarshal([]byte(validNMStateConfigBMH), &jsonMap)).To(Succeed())

		var networkConfigMap map[string]interface{}
		Expect(yaml.Unmarshal(config.NetworkConfig.Raw, &networkConfigMap)).To(Succeed())
		Expect(networkConfigMap).To(Equal(jsonMap))
	})

	It("fails if nmstate config is bad yaml", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		invalidNmstateString := "some\nnmstate\nstring"
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "netconfig",
				Namespace: bmh.Namespace,
			},
			Data: map[string][]byte{nmstateSecretKey: []byte(invalidNmstateString)},
		}
		Expect(c.Create(ctx, secret)).To(Succeed())

		bmh.Spec.PreprovisioningNetworkDataName = "netconfig"
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
	})

	It("fails when a referenced nmstate secret is missing", func() {
		// note that we don't create the netconfig secret.
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Spec.PreprovisioningNetworkDataName = "netconfig"
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}

		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
	})

	It("fails when the referenced nmstate secret is missing the required key", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "netconfig",
				Namespace: bmh.Namespace,
			},
			Data: map[string][]byte{"wrongKey": []byte(validNMStateConfigBMH)},
		}
		Expect(c.Create(ctx, secret)).To(Succeed())
		bmh.Spec.PreprovisioningNetworkDataName = "netconfig"
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
	})

	It("when a referenced clusterdeployment is missing", func() {
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Message).To(Equal("clusterDeployment with name 'test-cluster' in namespace 'test-namespace' not found"))
	})

	It("creates extra manifests", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "manifests",
				Namespace: clusterInstallNamespace,
			},
			Data: map[string]string{
				"manifest1.yaml": "thing: stuff",
				"manifest2.yaml": "other: foo",
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		clusterInstall.Spec.ExtraManifestsRefs = []corev1.LocalObjectReference{
			{Name: "manifests"},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		validateExtraManifestContent("manifest1.yaml", "thing: stuff")
		validateExtraManifestContent("manifest2.yaml", "other: foo")
	})

	It("validates extra manifests", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "manifests",
				Namespace: clusterInstallNamespace,
			},
			Data: map[string]string{
				"manifest1.yaml": "thing: \"st\"uff",
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		clusterInstall.Spec.ExtraManifestsRefs = []corev1.LocalObjectReference{
			{Name: "manifests"},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
	})

	It("creates certificates", func() {
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		clusterDeployment.Spec.ClusterName = "test-cluster"
		clusterDeployment.Spec.BaseDomain = "redhat.com"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		// Verify the kubeconfig secret
		kubeconfigSecret := &corev1.Secret{}
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: clusterInstallNamespace, Name: clusterDeployment.Name + "-admin-kubeconfig"}, kubeconfigSecret)
		Expect(err).NotTo(HaveOccurred())
		_, exists := kubeconfigSecret.Data["kubeconfig"]
		Expect(exists).To(BeTrue())
	})

	It("creates kubeadmin password", func() {
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		clusterDeployment.Spec.ClusterName = "test-cluster"
		clusterDeployment.Spec.BaseDomain = "redhat.com"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		// Verify the kubeconfig secret
		kubeconfigSecret := &corev1.Secret{}
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: clusterInstallNamespace, Name: clusterDeployment.Name + "-admin-password"}, kubeconfigSecret)
		Expect(err).NotTo(HaveOccurred())
		username, exists := kubeconfigSecret.Data["username"]
		Expect(exists).To(BeTrue())
		Expect(string(username)).To(Equal(credentials.DefaultUser))
		_, exists = kubeconfigSecret.Data["password"]
		Expect(exists).To(BeTrue())

	})

	It("configures a referenced BMH with state registering and ExternallyProvisioned true", func() {
		bmh := bmhInState(bmh_v1alpha1.StateRegistering)
		bmh.Spec.ExternallyProvisioned = true
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Spec.Image).To(BeNil())
		// We don't update the power state of a BMH with registering provisioning state
		Expect(bmh.Spec.Online).To(BeFalse())
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))

		dataImage := bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))
	})

	It("configures a referenced BMH with state available, ExternallyProvisioned false and online true", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Spec.Online = true
		bmh.Spec.ExternallyProvisioned = false
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Spec.Image).To(BeNil())
		Expect(bmh.Spec.ExternallyProvisioned).To(BeTrue())
		Expect(bmh.Spec.Online).To(BeTrue())
		Expect(bmh.Annotations).To(HaveKey(rebootAnnotation))
		Expect(bmh.Annotations).To(HaveKey(ibioManagedBMH))
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))

		dataImage := bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))
	})

	It("configures a referenced BMH with state available, ExternallyProvisioned false and online false", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Spec.Online = false
		bmh.Spec.ExternallyProvisioned = false
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Spec.Image).To(BeNil())
		Expect(bmh.Spec.ExternallyProvisioned).To(BeTrue())
		Expect(bmh.Spec.Online).To(BeTrue())
		Expect(bmh.Annotations).To(HaveKey(rebootAnnotation))
		Expect(bmh.Annotations).To(HaveKey(ibioManagedBMH))
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))

		dataImage := bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))
	})

	It("configures a referenced BMH with state externally provisioned and online false", func() {
		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)
		bmh.Spec.ExternallyProvisioned = true
		bmh.Status.PoweredOn = false
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Spec.Image).To(BeNil())
		Expect(bmh.Spec.Online).To(BeTrue())
		Expect(bmh.Annotations).To(HaveKey(rebootAnnotation))
		Expect(bmh.Annotations).To(HaveKey(ibioManagedBMH))
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))

		dataImage := bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))
	})

	It("configures a referenced BMH with status externally provisioned and online true", func() {
		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)
		bmh.Spec.ExternallyProvisioned = true
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Spec.Image).To(BeNil())
		Expect(bmh.Spec.Online).To(BeTrue())
		Expect(bmh.Annotations).To(HaveKey(rebootAnnotation))
		Expect(bmh.Annotations).To(HaveKey(ibioManagedBMH))
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))

		dataImage := bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))
	})

	It("sets the BMH ref in the cluster install status", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: clusterInstall.Namespace,
			Name:      clusterInstall.Name,
		}
		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		Expect(clusterInstall.Status.BareMetalHostRef).To(HaveValue(Equal(*clusterInstall.Spec.BareMetalHostRef)))
	})
	It("sets disables AutomatedCleaningMode", func() {
		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Spec.AutomatedCleaningMode).To(Equal(bmh_v1alpha1.CleaningModeDisabled))
	})

	It("doesn't error for a missing imageclusterinstall", func() {
		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
	})

	It("doesn't error when ClusterDeploymentRef is unset", func() {
		clusterInstall.Spec.ClusterDeploymentRef = nil
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Message).To(Equal("clusterDeploymentRef is unset"))

	})

	It("sets the ClusterInstallRequirementsMet condition to false when the bmhRef is missing", func() {
		clusterInstall.Spec.BareMetalHostRef = nil
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Reason).To(Equal(v1alpha1.HostValidationPending))
		Expect(cond.Message).To(Equal("No BareMetalHostRef set, nothing to do without provided bmh"))
	})

	It("Set ClusterInstallRequirementsMet to false in case there is not actual bmh under the reference", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      "doesntExist",
			Namespace: bmh.Namespace,
		}
		clusterInstall.Spec.MachineNetwork = "192.168.1.0/24"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Reason).To(Equal(v1alpha1.HostValidationPending))
		Expect(cond.Message).To(Equal("baremetalhosts.metal3.io \"doesntExist\" not found"))
	})

	It("updates the cluster install and cluster deployment metadata", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		clusterInstall.Spec.Hostname = "thing"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		clusterDeployment.Spec.BaseDomain = "example.com"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(ClusterConfigDir, imageBasedConfigFilename))
		Expect(err).NotTo(HaveOccurred())
		config := &imagebased.Config{}
		Expect(json.Unmarshal(content, config)).To(Succeed())

		updatedICI := v1alpha1.ImageClusterInstall{}
		Expect(c.Get(ctx, key, &updatedICI)).To(Succeed())
		meta := updatedICI.Spec.ClusterMetadata
		Expect(meta).ToNot(BeNil())
		Expect(meta.ClusterID).To(Equal(config.ClusterID))
		Expect(meta.InfraID).To(HavePrefix("thingcluster"))
		Expect(meta.InfraID).To(Equal(config.InfraID))
		Expect(meta.AdminKubeconfigSecretRef.Name).To(Equal("test-cluster-admin-kubeconfig"))
		Expect(meta.AdminPasswordSecretRef.Name).To(Equal("test-cluster-admin-password"))
	})

	It("succeeds in case bmh has ip in provided machine network", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Status.HardwareDetails.NIC = []bmh_v1alpha1.NIC{{IP: "192.168.1.30"}}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		clusterInstall.Spec.MachineNetwork = "192.168.1.0/24"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		installerSuccess()
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())

		content, err := os.ReadFile(outputFilePath(ClusterConfigDir, installConfigFilename))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &installertypes.InstallConfig{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())

		Expect(infoOut.BaseDomain).To(Equal(clusterDeployment.Spec.BaseDomain))
		Expect(infoOut.ObjectMeta.Name).To(Equal(clusterDeployment.Spec.ClusterName))
		Expect(infoOut.MachineNetwork[0].CIDR.String()).To(Equal(clusterInstall.Spec.MachineNetwork))
	})

	It("succeeds in case bmh has disabled inspection and no hw details", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Status.HardwareDetails = nil
		if bmh.ObjectMeta.Annotations == nil {
			bmh.ObjectMeta.Annotations = make(map[string]string)
		}
		bmh.ObjectMeta.Annotations[inspectAnnotation] = "disabled"
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		clusterInstall.Spec.MachineNetwork = "192.168.1.0/24"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		installerSuccess()
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())

		content, err := os.ReadFile(outputFilePath(ClusterConfigDir, installConfigFilename))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &installertypes.InstallConfig{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())

		Expect(infoOut.BaseDomain).To(Equal(clusterDeployment.Spec.BaseDomain))
		Expect(infoOut.ObjectMeta.Name).To(Equal(clusterDeployment.Spec.ClusterName))
		Expect(infoOut.MachineNetwork[0].CIDR.String()).To(Equal(clusterInstall.Spec.MachineNetwork))
	})

	It("in case there is no actual bmh under the reference we should not return error", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		clusterInstall.Spec.MachineNetwork = "192.168.1.0/24"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())
	})

	It("reque in case bmh has no hw details but after adding them it succeeds", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Status.HardwareDetails = nil
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		clusterInstall.Spec.MachineNetwork = "192.168.1.0/24"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(Equal(ctrl.Result{}))
		Expect(res.RequeueAfter).To(Equal(30 * time.Second))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Reason).To(Equal(v1alpha1.HostValidationPending))

		// good one
		Expect(c.Get(ctx, types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}, bmh)).To(Succeed())

		bmh.Status.HardwareDetails = &bmh_v1alpha1.HardwareDetails{NIC: []bmh_v1alpha1.NIC{
			{IP: "192.168.50.30"},
			{IP: "192.168.1.30"}}}

		Expect(c.Update(ctx, bmh)).To(Succeed())
		installerSuccess()
		_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())

		content, err := os.ReadFile(outputFilePath(ClusterConfigDir, installConfigFilename))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &installertypes.InstallConfig{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())

		Expect(infoOut.BaseDomain).To(Equal(clusterDeployment.Spec.BaseDomain))
		Expect(infoOut.ObjectMeta.Name).To(Equal(clusterDeployment.Spec.ClusterName))
		Expect(infoOut.MachineNetwork[0].CIDR.String()).To(Equal(clusterInstall.Spec.MachineNetwork))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
		Expect(cond.Reason).To(Equal(v1alpha1.HostValidationSucceeded))
	})

	It("fails in case bmh has no ip in provided machine network but after changing machine network it succeeds", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Status.HardwareDetails.NIC = []bmh_v1alpha1.NIC{{IP: "192.168.1.30"}}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		// bad machine network
		clusterInstall.Spec.MachineNetwork = "192.168.5.0/24"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("bmh host doesn't have any nic with ip in provided"))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
		Expect(cond.Reason).To(Equal(v1alpha1.HostValidationFailedReason))

		// good one
		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		clusterInstall.Spec.MachineNetwork = "192.168.1.0/24"
		Expect(c.Update(ctx, clusterInstall)).To(Succeed())

		installerSuccess()
		_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())

		content, err := os.ReadFile(outputFilePath(ClusterConfigDir, installConfigFilename))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &installertypes.InstallConfig{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())

		Expect(infoOut.BaseDomain).To(Equal(clusterDeployment.Spec.BaseDomain))
		Expect(infoOut.ObjectMeta.Name).To(Equal(clusterDeployment.Spec.ClusterName))
		Expect(infoOut.MachineNetwork[0].CIDR.String()).To(Equal(clusterInstall.Spec.MachineNetwork))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
		Expect(cond.Reason).To(Equal(v1alpha1.HostValidationSucceeded))
	})

	It("labels secrets for backup", func() {
		clusterInstall.Spec.ClusterMetadata = &hivev1.ClusterMetadata{
			AdminKubeconfigSecretRef: corev1.LocalObjectReference{Name: clusterDeployment.Name + "-admin-kubeconfig"},
			AdminPasswordSecretRef:   &corev1.LocalObjectReference{Name: clusterDeployment.Name + "-admin-password"},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		clusterDeployment.Spec.ClusterName = "test"
		clusterDeployment.Spec.BaseDomain = "example.com"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		secretRefs := []types.NamespacedName{
			{Namespace: pullSecret.Namespace, Name: pullSecret.Name},
			{Namespace: clusterInstallNamespace, Name: clusterInstall.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name},
			{Namespace: clusterInstallNamespace, Name: clusterInstall.Spec.ClusterMetadata.AdminPasswordSecretRef.Name},
		}
		for _, key := range secretRefs {
			testSecret := &corev1.Secret{}
			Expect(c.Get(ctx, key, testSecret)).To(Succeed())
			Expect(testSecret.GetLabels()).To(HaveKeyWithValue(backupLabel, backupLabelValue), "Secret %s/%s missing annotation", testSecret.Namespace, testSecret.Name)
		}
	})

	It("labels configmaps for backup", func() {
		configMaps := []*corev1.ConfigMap{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "manifest1",
					Namespace: clusterInstallNamespace,
				},
				Data: map[string]string{
					"manifest1.yaml": "thing: stuff",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "manifest2",
					Namespace: clusterInstallNamespace,
				},
				Data: map[string]string{
					"manifest2.yaml": "other: foo",
				},
			},
		}
		clusterInstall.Spec.ExtraManifestsRefs = []corev1.LocalObjectReference{
			{Name: "manifest1"},
			{Name: "manifest2"},
		}
		configMaps = append(configMaps,
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ca-bundle",
					Namespace: "test-namespace",
				},
				Data: map[string]string{caBundleFileName: "mycabundle"},
			},
		)
		clusterInstall.Spec.CABundleRef = &corev1.LocalObjectReference{
			Name: "ca-bundle",
		}

		for _, cm := range configMaps {
			Expect(c.Create(ctx, cm)).To(Succeed())
		}

		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		for _, cm := range configMaps {
			testCM := &corev1.ConfigMap{}
			key := types.NamespacedName{
				Name:      cm.Name,
				Namespace: cm.Namespace,
			}
			Expect(c.Get(ctx, key, testCM)).To(Succeed())
			Expect(testCM.GetLabels()).To(HaveKeyWithValue(backupLabel, backupLabelValue), "ConfigMap %s/%s missing annotation", testCM.Namespace, testCM.Name)
		}
	})
})

var _ = Describe("Reconcile with DataImageCoolDownPeriod set to 1 second", func() {
	var (
		c                       client.Client
		mockCtrl                *gomock.Controller
		dataDir                 string
		r                       *ImageClusterInstallReconciler
		ctx                     = context.Background()
		clusterInstallName      = "test-cluster"
		clusterInstallNamespace = "test-namespace"
		clusterInstall          *v1alpha1.ImageClusterInstall
		clusterDeployment       *hivev1.ClusterDeployment
		pullSecret              *corev1.Secret
		installerMock           *installer.MockInstaller
		testPullSecretVal       = `{"auths":{"cloud.openshift.com":{"auth":"dXNlcjpwYXNzd29yZAo=","email":"r@r.com"}}}`
	)

	installerSuccess := func() {
		dir := filepath.Join(dataDir, "namespaces", clusterInstallNamespace, string(clusterInstall.ObjectMeta.UID), "files")
		installerMock.EXPECT().CreateInstallationIso(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil).Times(1).Do(func(any, any, any) {
			Expect(os.WriteFile(filepath.Join(dir, ClusterConfigDir, IsoName), []byte("test"), 0644)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(dir, ClusterConfigDir, authDir), 0700)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, ClusterConfigDir, authDir, kubeAdminFile), []byte("test"), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, ClusterConfigDir, authDir, credentials.Kubeconfig), []byte(kubeconfig), 0644)).To(Succeed())
		})
	}

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		installerMock = installer.NewMockInstaller(mockCtrl)
		fc := fakeclient.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&v1alpha1.ImageClusterInstall{}).
			Build()
		c = FakeClientWithTimestamp{Client: fc}
		var err error
		dataDir, err = os.MkdirTemp("", "imageclusterinstall_controller_test_data")
		Expect(err).NotTo(HaveOccurred())
		cm := credentials.Credentials{
			Client: c,
			Log:    logrus.New(),
			Scheme: scheme.Scheme,
		}
		r = &ImageClusterInstallReconciler{
			Client:      c,
			Credentials: cm,
			Scheme:      scheme.Scheme,
			Log:         logrus.New(),
			BaseURL:     "https://images-namespace.cluster.example.com",
			Options: &ImageClusterInstallReconcilerOptions{
				DataDir:                 dataDir,
				DataImageCoolDownPeriod: time.Second,
			},
			NoncachedClient: c,
			Installer:       installerMock,
		}

		imageSet := &hivev1.ClusterImageSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "imageset",
				Namespace: clusterInstallNamespace,
			},
			Spec: hivev1.ClusterImageSetSpec{
				ReleaseImage: "registry.example.com/releases/ocp@sha256:0ec9d715c717b2a592d07dd83860013613529fae69bc9eecb4b2d4ace679f6f3",
			},
		}
		Expect(c.Create(ctx, imageSet)).To(Succeed())
		pullSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ps",
				Namespace: clusterInstallNamespace,
			},
			Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(testPullSecretVal)},
		}
		Expect(c.Create(ctx, pullSecret)).To(Succeed())

		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-1",
				Namespace: "test-bmh-namespace",
			},
			Status: bmh_v1alpha1.BareMetalHostStatus{
				Provisioning: bmh_v1alpha1.ProvisionStatus{
					State: bmh_v1alpha1.StateExternallyProvisioned,
				},
				HardwareDetails: &bmh_v1alpha1.HardwareDetails{NIC: []bmh_v1alpha1.NIC{{IP: "1.1.1.1"}}},
				PoweredOn:       true,
			},
			Spec: bmh_v1alpha1.BareMetalHostSpec{
				ExternallyProvisioned: true,
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall = &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
				UID:        types.UID(uuid.New().String()),
			},
			Spec: v1alpha1.ImageClusterInstallSpec{
				ImageSetRef: hivev1.ClusterImageSetReference{
					Name: imageSet.Name,
				},
				ClusterDeploymentRef: &corev1.LocalObjectReference{Name: clusterInstallName},
				BareMetalHostRef: &v1alpha1.BareMetalHostReference{
					Name:      bmh.Name,
					Namespace: bmh.Namespace,
				},
			},
		}

		clusterDeployment = &hivev1.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterInstallName,
				Namespace: clusterInstallNamespace,
			},
			Spec: hivev1.ClusterDeploymentSpec{
				ClusterInstallRef: &hivev1.ClusterInstallLocalReference{
					Group:   clusterInstall.GroupVersionKind().Group,
					Version: clusterInstall.GroupVersionKind().Version,
					Kind:    clusterInstall.GroupVersionKind().Kind,
					Name:    clusterInstall.Name,
				},
				PullSecretRef: &corev1.LocalObjectReference{
					Name: "ps",
				},
			}}
	})

	AfterEach(func() {
		mockCtrl.Finish()
		Expect(os.RemoveAll(dataDir)).To(Succeed())
	})

	imageURL := func() string {
		return fmt.Sprintf("https://images-namespace.cluster.example.com/images/%s/%s.iso", clusterInstallNamespace, clusterInstall.ObjectMeta.UID)
	}

	It("configures a referenced BMH", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Spec.Online = true
		bmh.Spec.ExternallyProvisioned = false
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}
		installerSuccess()
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{RequeueAfter: time.Second}))
		time.Sleep(time.Second)
		res, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Spec.Image).To(BeNil())
		Expect(bmh.Spec.ExternallyProvisioned).To(BeTrue())
		Expect(bmh.Spec.Online).To(BeTrue())
		Expect(bmh.Annotations).To(HaveKey(rebootAnnotation))
		Expect(bmh.Annotations).To(HaveKey(ibioManagedBMH))
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))

		dataImage := bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))
	})
	It("configures a referenced BMH when dataImage already exists", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Spec.Online = true
		bmh.Spec.ExternallyProvisioned = false
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}

		dataImage := bmh_v1alpha1.DataImage{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
			Spec: bmh_v1alpha1.DataImageSpec{
				URL: fmt.Sprintf("https://images-namespace.cluster.example.com/images/%s/%s.iso", clusterInstallNamespace, clusterInstall.ObjectMeta.UID),
			},
		}
		Expect(c.Create(ctx, &dataImage)).To(Succeed())
		// wait a second between creating the dataImage and the reconcile
		time.Sleep(time.Second)
		installerSuccess()
		// This should work in one go without RequeueAfter
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Spec.Image).To(BeNil())
		Expect(bmh.Spec.ExternallyProvisioned).To(BeTrue())
		Expect(bmh.Spec.Online).To(BeTrue())
		Expect(bmh.Annotations).To(HaveKey(rebootAnnotation))
		Expect(bmh.Annotations).To(HaveKey(ibioManagedBMH))
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))

		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))
	})
})

var _ = Describe("mapBMHToICI", func() {
	var (
		c                       client.Client
		r                       *ImageClusterInstallReconciler
		ctx                     = context.Background()
		clusterInstallName      = "test-cluster-install"
		clusterInstallNamespace = "test-namespace"
	)

	BeforeEach(func() {
		fc := fakeclient.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&v1alpha1.ImageClusterInstall{}).
			// this update the client to add index for .spec.bareMetalHostRef.name and namespace as we do in the SetupWithManager
			WithIndex(&v1alpha1.ImageClusterInstall{}, ".spec.bareMetalHostRef.name", func(rawObj client.Object) []string {
				ici, ok := rawObj.(*v1alpha1.ImageClusterInstall)
				if !ok || ici.Spec.BareMetalHostRef == nil {
					return nil
				}
				return []string{ici.Spec.BareMetalHostRef.Name}
			}).
			WithIndex(&v1alpha1.ImageClusterInstall{}, ".spec.bareMetalHostRef.namespace", func(rawObj client.Object) []string {
				ici, ok := rawObj.(*v1alpha1.ImageClusterInstall)
				if !ok || ici.Spec.BareMetalHostRef == nil {
					return nil
				}
				return []string{ici.Spec.BareMetalHostRef.Namespace}
			}).
			Build()
		c = FakeClientWithTimestamp{Client: fc}

		r = &ImageClusterInstallReconciler{
			Client: c,
			Scheme: scheme.Scheme,
			Log:    logrus.New(),
		}
	})

	It("returns a request for the cluster install referencing the given BMH", func() {
		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall := &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterInstallName,
				Namespace: clusterInstallNamespace,
			},
			Spec: v1alpha1.ImageClusterInstallSpec{
				BareMetalHostRef: &v1alpha1.BareMetalHostReference{
					Name:      bmh.Name,
					Namespace: bmh.Namespace,
				},
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterInstall = &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-cluster-install",
				Namespace: clusterInstallNamespace,
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		requests := r.mapBMHToICI(ctx, bmh)
		Expect(len(requests)).To(Equal(1))
		Expect(requests[0].NamespacedName).To(Equal(types.NamespacedName{
			Name:      clusterInstallName,
			Namespace: clusterInstallNamespace,
		}))
	})

	It("returns an empty list when no cluster install matches", func() {
		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall := &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterInstallName,
				Namespace: clusterInstallNamespace,
			},
			Spec: v1alpha1.ImageClusterInstallSpec{
				BareMetalHostRef: &v1alpha1.BareMetalHostReference{
					Name:      "other-bmh",
					Namespace: bmh.Namespace,
				},
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterInstall = &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-cluster-install",
				Namespace: clusterInstallNamespace,
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		requests := r.mapBMHToICI(ctx, bmh)
		Expect(len(requests)).To(Equal(0))
	})
})

var _ = Describe("mapCDToICI", func() {
	var (
		c                       client.Client
		r                       *ImageClusterInstallReconciler
		ctx                     = context.Background()
		clusterInstallName      = "test-cluster-install"
		clusterInstallNamespace = "test-namespace"
	)

	BeforeEach(func() {
		fc := fakeclient.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&v1alpha1.ImageClusterInstall{}).
			Build()
		c = FakeClientWithTimestamp{Client: fc}
		r = &ImageClusterInstallReconciler{
			Client: c,
			Scheme: scheme.Scheme,
			Log:    logrus.New(),
		}
	})

	It("returns a request for the cluster install referenced by the given ClusterDeployment", func() {
		cd := &hivev1.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cd",
				Namespace: clusterInstallNamespace,
			},
			Spec: hivev1.ClusterDeploymentSpec{
				ClusterInstallRef: &hivev1.ClusterInstallLocalReference{
					Group:   v1alpha1.Group,
					Version: v1alpha1.Version,
					Kind:    "ImageClusterInstall",
					Name:    clusterInstallName,
				},
			},
		}
		Expect(c.Create(ctx, cd)).To(Succeed())

		requests := r.mapCDToICI(ctx, cd)
		Expect(len(requests)).To(Equal(1))
		Expect(requests[0].NamespacedName).To(Equal(types.NamespacedName{
			Name:      clusterInstallName,
			Namespace: clusterInstallNamespace,
		}))
	})

	It("returns an empty list when no cluster install is set", func() {
		cd := &hivev1.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cd",
				Namespace: clusterInstallNamespace,
			},
			Spec: hivev1.ClusterDeploymentSpec{},
		}
		Expect(c.Create(ctx, cd)).To(Succeed())

		requests := r.mapCDToICI(ctx, cd)
		Expect(len(requests)).To(Equal(0))
	})

	It("returns an empty list when the cluster install does not match our GVK", func() {
		cd := &hivev1.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cd",
				Namespace: clusterInstallNamespace,
			},
			Spec: hivev1.ClusterDeploymentSpec{
				ClusterInstallRef: &hivev1.ClusterInstallLocalReference{
					Group:   "extensions.hive.openshift.io",
					Version: "v1beta2",
					Kind:    "AgentClusterInstall",
					Name:    clusterInstallName,
				},
			},
		}
		Expect(c.Create(ctx, cd)).To(Succeed())

		requests := r.mapCDToICI(ctx, cd)
		Expect(len(requests)).To(Equal(0))
	})
})

var _ = Describe("handleFinalizer", func() {
	var (
		c                       client.Client
		dataDir                 string
		r                       *ImageClusterInstallReconciler
		ctx                     = context.Background()
		clusterInstallName      = "test-cluster-install"
		clusterInstallNamespace = "test-namespace"
	)

	BeforeEach(func() {
		fc := fakeclient.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&v1alpha1.ImageClusterInstall{}).
			Build()
		c = FakeClientWithTimestamp{Client: fc}
		var err error
		dataDir, err = os.MkdirTemp("", "imageclusterinstall_controller_test_data")
		Expect(err).NotTo(HaveOccurred())

		r = &ImageClusterInstallReconciler{
			Client: c,
			Scheme: scheme.Scheme,
			Log:    logrus.New(),
			Options: &ImageClusterInstallReconcilerOptions{
				DataDir:                 dataDir,
				DataImageCoolDownPeriod: time.Duration(0),
			},
		}
	})

	AfterEach(func() {
		Expect(os.RemoveAll(dataDir)).To(Succeed())
	})

	It("adds the finalizer if the cluster install is not being deleted", func() {
		clusterInstall := &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterInstallName,
				Namespace: clusterInstallNamespace,
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		res, stop, err := r.handleFinalizer(ctx, r.Log, clusterInstall)
		Expect(res).To(Equal(ctrl.Result{Requeue: true}))
		Expect(stop).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		key := types.NamespacedName{
			Name:      clusterInstallName,
			Namespace: clusterInstallNamespace,
		}
		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		Expect(clusterInstall.GetFinalizers()).To(ContainElement(clusterInstallFinalizerName))
	})

	It("noops if the finalizer is already present", func() {
		clusterInstall := &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		res, stop, err := r.handleFinalizer(ctx, r.Log, clusterInstall)
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(stop).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	})

	It("deletes the local files when the config is deleted", func() {
		clusterInstall := &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		// mark clusterInstall as deleted to call the finalizer handler
		now := metav1.Now()
		clusterInstall.ObjectMeta.DeletionTimestamp = &now

		filesDir := filepath.Join(dataDir, "namespaces", clusterInstall.Namespace, clusterInstall.Name, "files")
		testFilePath := filepath.Join(filesDir, "testfile")
		Expect(os.MkdirAll(filesDir, 0700)).To(Succeed())
		Expect(os.WriteFile(testFilePath, []byte("stuff"), 0644)).To(Succeed())

		res, stop, err := r.handleFinalizer(ctx, r.Log, clusterInstall)
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(stop).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		_, err = os.Stat(testFilePath)
		Expect(os.IsNotExist(err)).To(BeTrue())

		key := types.NamespacedName{
			Name:      clusterInstallName,
			Namespace: clusterInstallNamespace,
		}
		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		Expect(clusterInstall.GetFinalizers()).ToNot(ContainElement(clusterInstallFinalizerName))
	})

	It("removes the finalizer if the referenced BMH doesn't exist", func() {
		clusterInstall := &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
			},
			Spec: v1alpha1.ImageClusterInstallSpec{
				BareMetalHostRef: &v1alpha1.BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		dataImage := bmh_v1alpha1.DataImage{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
			Spec: bmh_v1alpha1.DataImageSpec{
				URL: fmt.Sprintf("https://images-namespace.cluster.example.com/images/%s/%s.iso", clusterInstallNamespace, clusterInstall.ObjectMeta.UID),
			},
		}
		Expect(c.Create(ctx, &dataImage)).To(Succeed())

		// mark clusterInstall as deleted to call the finalizer handler
		now := metav1.Now()
		clusterInstall.ObjectMeta.DeletionTimestamp = &now

		res, stop, err := r.handleFinalizer(ctx, r.Log, clusterInstall)
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(stop).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		clusterInstallKey := types.NamespacedName{
			Name:      clusterInstallName,
			Namespace: clusterInstallNamespace,
		}
		Expect(c.Get(ctx, clusterInstallKey, clusterInstall)).To(Succeed())
		Expect(clusterInstall.GetFinalizers()).ToNot(ContainElement(clusterInstallFinalizerName))
		key := types.NamespacedName{
			Namespace: dataImage.Namespace,
			Name:      dataImage.Name,
		}
		dataImage = bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(HaveOccurred())

	})

	It("removes dataimage on ici delete", func() {
		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)
		bmh.Spec.Online = true
		setAnnotationIfNotExists(&bmh.ObjectMeta, detachedAnnotation, detachedAnnotationValue)

		clusterInstall := &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
			},
			Spec: v1alpha1.ImageClusterInstallSpec{
				BareMetalHostRef: &v1alpha1.BareMetalHostReference{
					Name:      bmh.Name,
					Namespace: bmh.Namespace,
				},
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		dataImage := bmh_v1alpha1.DataImage{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bmh.Name,
				Namespace: bmh.Namespace,
			},
			Spec: bmh_v1alpha1.DataImageSpec{
				URL: fmt.Sprintf("https://images-namespace.cluster.example.com/images/%s/%s.iso", clusterInstallNamespace, clusterInstall.ObjectMeta.UID),
			},
		}
		Expect(c.Create(ctx, &dataImage)).To(Succeed())

		// Mark clusterInstall as deleted to call the finalizer handler
		now := metav1.Now()
		clusterInstall.ObjectMeta.DeletionTimestamp = &now

		// First round will delete the dataImage (mark for deletion) and ask the bmh to reboot
		res, stop, err := r.handleFinalizer(ctx, r.Log, clusterInstall)
		Expect(res).To(Equal(ctrl.Result{RequeueAfter: 1 * time.Minute}))
		Expect(stop).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		clusterInstallKey := types.NamespacedName{
			Name:      clusterInstallName,
			Namespace: clusterInstallNamespace,
		}
		// Validate the finalizer wasn't removed yet
		Expect(c.Get(ctx, clusterInstallKey, clusterInstall)).To(Succeed())
		Expect(clusterInstall.GetFinalizers()).To(ContainElement(clusterInstallFinalizerName))

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}

		// Validate the bmh is now attached and got the reboot annotation
		bmh = &bmh_v1alpha1.BareMetalHost{}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))
		Expect(bmh.Annotations).To(HaveKey(rebootAnnotation))

		dataImage = bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(HaveOccurred())
		// Once the dataImage is deleted the finalizer should be removed

		// Mark clusterInstall as deleted to call the finalizer handler
		clusterInstall.ObjectMeta.DeletionTimestamp = &now
		res, stop, err = r.handleFinalizer(ctx, r.Log, clusterInstall)
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(stop).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		// Validate the finalizer get removed after the data image is deleted
		Expect(c.Get(ctx, clusterInstallKey, clusterInstall)).To(Succeed())
		Expect(clusterInstall.GetFinalizers()).ToNot(ContainElement(clusterInstallFinalizerName))
	})
})
