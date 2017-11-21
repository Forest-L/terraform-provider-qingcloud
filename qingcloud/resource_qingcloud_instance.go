package qingcloud

import (
	"fmt"

	"github.com/hashicorp/terraform/helper/schema"
	qc "github.com/yunify/qingcloud-sdk-go/service"
)

func resourceQingcloudInstance() *schema.Resource {
	return &schema.Resource{
		Create: resourceQingcloudInstanceCreate,
		Read:   resourceQingcloudInstanceRead,
		Update: resourceQingcloudInstanceUpdate,
		Delete: resourceQingcloudInstanceDelete,
		Schema: map[string]*schema.Schema{
			resourceName: &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			resourceDescription: &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"image_id": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"cpu": &schema.Schema{
				Type:         schema.TypeInt,
				Required:     true,
				ValidateFunc: withinArrayInt(1, 2, 4, 8, 16),
				Default:      1,
			},
			"memory": &schema.Schema{
				Type:         schema.TypeInt,
				Required:     true,
				ValidateFunc: withinArrayInt(1024, 2048, 4096, 6144, 8192, 12288, 16384, 24576, 32768),
				Default:      1024,
			},
			"managed_vxnet_id": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"static_ip": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"keypair_ids": &schema.Schema{
				Type:     schema.TypeSet,
				Required: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
				Set:      schema.HashString,
			},
			"security_group_id": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"eip_id": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"volume_ids": &schema.Schema{
				Type:     schema.TypeSet,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
				Set:      schema.HashString,
			},
			"public_ip": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},
			"private_ip": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},
			resourceTagIds:   tagIdsSchema(),
			resourceTagNames: tagNamesSchema(),
		},
	}
}

func resourceQingcloudInstanceCreate(d *schema.ResourceData, meta interface{}) error {
	clt := meta.(*QingCloudClient).instance
	input := new(qc.RunInstancesInput)
	input.Count = qc.Int(1)
	input.InstanceName, _ = getNamePointer(d)
	input.ImageID = qc.String(d.Get("image_id").(string))
	input.CPU = qc.Int(d.Get("cpu").(int))
	input.Memory = qc.Int(d.Get("memory").(int))
	input.SecurityGroup = qc.String(d.Get("security_group_id").(string))
	input.LoginMode = qc.String("keypair")
	kps := d.Get("keypair_ids").(*schema.Set).List()
	if len(kps) > 0 {
		kp := kps[0].(string)
		input.LoginKeyPair = qc.String(kp)
	}
	output, err := clt.RunInstances(input)
	if err != nil {
		return err
	}
	d.SetId(qc.StringValue(output.Instances[0]))
	if _, err := InstanceTransitionStateRefresh(clt, d.Id()); err != nil {
		return err
	}
	return resourceQingcloudVxnetUpdate(d, meta)
}

func resourceQingcloudInstanceRead(d *schema.ResourceData, meta interface{}) error {
	clt := meta.(*QingCloudClient).instance
	input := new(qc.DescribeInstancesInput)
	input.Instances = []*string{qc.String(d.Id())}
	input.Verbose = qc.Int(1)
	output, err := clt.DescribeInstances(input)
	if err != nil {
		return fmt.Errorf("Error describe instance: %s", err)
	}
	if output.RetCode != nil && qc.IntValue(output.RetCode) != 0 {
		return fmt.Errorf("Error describe instance: %s", *output.Message)
	}
	if len(output.InstanceSet) == 0 {
		d.SetId("")
		return nil
	}
	instance := output.InstanceSet[0]
	d.Set(resourceName, qc.StringValue(instance.InstanceName))
	d.Set("image_id", qc.StringValue(instance.Image.ImageID))
	d.Set(resourceDescription, qc.StringValue(instance.Description))
	d.Set("cpu", qc.IntValue(instance.VCPUsCurrent))
	d.Set("memory", qc.IntValue(instance.MemoryCurrent))
	if instance.VxNets != nil && len(instance.VxNets) > 0 {
		vxnet := instance.VxNets[0]
		if qc.IntValue(vxnet.VxNetType) == 2 {
			d.Set("managed_vxnet_id", "vxnet-0")
		} else {
			d.Set("managed_vxnet_id", qc.StringValue(vxnet.VxNetID))
		}
		d.Set("private_ip", qc.StringValue(vxnet.PrivateIP))
		if d.Get("static_ip") != "" {
			d.Set("static_ip", qc.StringValue(vxnet.PrivateIP))
		}
	} else {
		d.Set("vxnet_id", "")
		d.Set("private_ip", "")
	}
	if instance.EIP != nil {
		d.Set("eip_id", qc.StringValue(instance.EIP.EIPID))
		d.Set("public_ip", qc.StringValue(instance.EIP.EIPAddr))
	}
	if instance.SecurityGroup != nil {
		d.Set("security_group_id", qc.StringValue(instance.SecurityGroup.SecurityGroupID))
	}
	if instance.KeyPairIDs != nil {
		keypairIDs := make([]string, 0, len(instance.KeyPairIDs))
		for _, kp := range instance.KeyPairIDs {
			keypairIDs = append(keypairIDs, qc.StringValue(kp))
		}
		d.Set("keypair_ids", keypairIDs)
	}
	resourceSetTag(d, instance.Tags)
	return nil
}

func resourceQingcloudInstanceUpdate(d *schema.ResourceData, meta interface{}) error {
	// clt := meta.(*QingCloudClient).instance
	err := modifyInstanceAttributes(d, meta)
	if err != nil {
		return err
	}
	// change vxnet
	err = instanceUpdateChangeManagedVxNet(d, meta)
	if err != nil {
		return err
	}
	// change security_group
	err = instanceUpdateChangeSecurityGroup(d, meta)
	if err != nil {
		return err
	}
	// change eip
	err = instanceUpdateChangeEip(d, meta)
	if err != nil {
		return err
	}
	// change keypair
	err = instanceUpdateChangeKeyPairs(d, meta)
	if err != nil {
		return err
	}
	// resize instance
	err = instanceUpdateResize(d, meta)
	if err != nil {
		return err
	}
	if err := resourceUpdateTag(d, meta, qingcloudResourceTypeInstance); err != nil {
		return err
	}
	return resourceQingcloudInstanceRead(d, meta)
}

func resourceQingcloudInstanceDelete(d *schema.ResourceData, meta interface{}) error {
	clt := meta.(*QingCloudClient).instance
	// dissociate eip before leave vxnet
	if _, err := deleteInstanceDissociateEip(d, meta); err != nil {
		return err
	}
	if _, err := InstanceTransitionStateRefresh(clt, d.Id()); err != nil {
		return err
	}
	_, err := deleteInstanceLeaveVxnet(d, meta)
	if err != nil {
		return err
	}
	if _, err := InstanceNetworkTransitionStateRefresh(clt, d.Id()); err != nil {
		return err
	}
	input := new(qc.TerminateInstancesInput)
	input.Instances = []*string{qc.String(d.Id())}
	_, err = clt.TerminateInstances(input)
	if err != nil {
		return fmt.Errorf("Error terminate instance: %s", err)
	}
	if _, err := InstanceTransitionStateRefresh(clt, d.Id()); err != nil {
		return err
	}
	d.SetId("")
	return nil
}
