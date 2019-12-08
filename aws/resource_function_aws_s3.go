package aws

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"

	"errors"
	"github.com/alessandromr/go-aws-serverless/services/function"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/helper/validation"
	"log"
)

const awsMutexLambdaKey = `aws_lambda_function`

var validS3Events = []string{
	"s3:ObjectCreated:*",
	"s3:ObjectCreated:Put",
	"s3:ObjectCreated:Post",
	"s3:ObjectCreated:Copy",
	"s3:ObjectCreated:CompleteMultipartUpload",
	"s3:ObjectRemoved:*",
	"s3:ObjectRemoved:Delete",
	"s3:ObjectRemoved:DeleteMarkerCreated",
	"s3:ObjectRestore:Post",
	"s3:ObjectRestore:Completed",
	"s3:ReducedRedundancyLostObject",
	"s3:Replication:OperationFailedReplication",
	"s3:Replication:OperationMissedThreshold",
	"s3:Replication:OperationReplicatedAfterThreshold",
	"s3:Replication:OperationNotTracked",
}

func ResourceFunctionS3() *schema.Resource {
	return &schema.Resource{
		Create: resourceFunctionS3Create,
		Read:   resourceFunctionS3Read,
		Update: resourceFunctionS3Update,
		Delete: resourceFunctionS3Delete,

		Schema: map[string]*schema.Schema{
			"function": {
				Type:     schema.TypeList,
				Optional: false,
				MinItems: 1,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"filename": {
							Type:          schema.TypeString,
							Optional:      true,
							ConflictsWith: []string{"s3_bucket", "s3_key", "s3_object_version"},
						},
						"s3_bucket": {
							Type:          schema.TypeString,
							Optional:      true,
							ConflictsWith: []string{"filename"},
						},
						"s3_key": {
							Type:          schema.TypeString,
							Optional:      true,
							ConflictsWith: []string{"filename"},
						},
						"s3_object_version": {
							Type:          schema.TypeString,
							Optional:      true,
							ConflictsWith: []string{"filename"},
						},
						"description": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"memory_size": {
							Type:     schema.TypeInt,
							Optional: true,
							Default:  128,
						},
						"runtime": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validation.StringInSlice(validLambdaRuntimes, false),
						},
						"environment": {
							Type:     schema.TypeList,
							Optional: true,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"variables": {
										Type:     schema.TypeMap,
										Optional: true,
										Elem:     &schema.Schema{Type: schema.TypeString},
									},
								},
							},
						},
						"timeout": {
							Type:     schema.TypeInt,
							Optional: true,
							Default:  3,
						},
						"vpc_config": {
							Type:     schema.TypeList,
							Optional: true,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"subnet_ids": {
										Type:     schema.TypeSet,
										Required: true,
										Elem:     &schema.Schema{Type: schema.TypeString},
										Set:      schema.HashString,
									},
									"security_group_ids": {
										Type:     schema.TypeSet,
										Required: true,
										Elem:     &schema.Schema{Type: schema.TypeString},
										Set:      schema.HashString,
									},
									"vpc_id": {
										Type:     schema.TypeString,
										Computed: true,
									},
								},
							},
						},
						"function_name": {
							Type:     schema.TypeString,
							Required: true,
							ForceNew: true,
						},
						"handler": {
							Type:     schema.TypeString,
							Required: true,
						},
						"arn": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"last_modified": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"source_code_hash": {
							Type:     schema.TypeString,
							Optional: true,
							Computed: true,
						},
						"source_code_size": {
							Type:     schema.TypeInt,
							Computed: true,
						},
						"qualified_arn": {}, //ToDo
						"invoke_arn":    {}, //ToDo
						// "role": {
						// },
						// "layers": {
						// 	Type:     schema.TypeList,
						// 	Optional: true,
						// 	MaxItems: 5,
						// 	Elem: &schema.Schema{
						// 		Type:         schema.TypeString,
						// 		ValidateFunc: validateArn,
						// 	},
						// },
					},
				},
			},
			"event": {
				Type:     schema.TypeList,
				Optional: false,
				MinItems: 1,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{

						"bucket": {
							Type:     schema.TypeString,
							Required: true,
						},
						"event_types": {
							Type:     schema.TypeList,
							Required: true,
							MinItems: 1,
							MaxItems: 10,
							Elem: &schema.Schema{
								Type:         schema.TypeString,
								ValidateFunc: validation.StringInSlice(validS3Events, false),
							},
						},
						"event_key": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"object_prefix": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"object_suffix": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"bucket_domain_name": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"bucket_regional_domain_name": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"arn": {
							Type:     schema.TypeString,
							Optional: true,
							Computed: true,
						},
					},
				},
			},
			"role": {
				Type:     schema.TypeList,
				Optional: false,
				MinItems: 1,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"additional_policy": {},
					},
				},
			},
		},
	}
}

func resourceFunctionS3Create(d *schema.ResourceData, m interface{}) error {
	functionName := d.Get("function_name").(string)
	// reservedConcurrentExecutions := d.Get("reserved_concurrent_executions").(int)
	log.Printf("[DEBUG] Creating Serverless AWS Function %s", functionName)

	filename, hasFilename := d.GetOk("filename")
	s3Bucket, bucketOk := d.GetOk("s3_bucket")
	s3Key, keyOk := d.GetOk("s3_key")
	s3ObjectVersion, versionOk := d.GetOk("s3_object_version")

	if !hasFilename && !bucketOk && !keyOk && !versionOk {
		return errors.New("filename or s3_* attributes must be set")
	}

	var functionCode *lambda.FunctionCode
	if hasFilename {
		// Grab an exclusive lock so that we're only reading one function into
		// memory at a time.
		// See https://github.com/hashicorp/terraform/issues/9364
		awsMutexKV.Lock(awsMutexLambdaKey)
		defer awsMutexKV.Unlock(awsMutexLambdaKey)
		file, err := loadFileContent(filename.(string))
		if err != nil {
			return fmt.Errorf("Unable to load %q: %s", filename.(string), err)
		}
		functionCode = &lambda.FunctionCode{
			ZipFile: file,
		}
	} else {
		if !bucketOk || !keyOk {
			return errors.New("s3_bucket and s3_key must all be set while using S3 code source")
		}
		functionCode = &lambda.FunctionCode{
			S3Bucket: aws.String(s3Bucket.(string)),
			S3Key:    aws.String(s3Key.(string)),
		}
		if versionOk {
			functionCode.S3ObjectVersion = aws.String(s3ObjectVersion.(string))
		}
	}

	funcParam := &lambda.CreateFunctionInput{
		Code:         functionCode,
		Description:  aws.String(d.Get("description").(string)),
		FunctionName: aws.String(functionName),
		Handler:      aws.String(d.Get("handler").(string)),
		MemorySize:   aws.Int64(int64(d.Get("memory_size").(int))),
		// Role:         aws.String(iamRole),
		Runtime: aws.String(d.Get("runtime").(string)),
		Timeout: aws.Int64(int64(d.Get("timeout").(int))),
		Publish: aws.Bool(d.Get("publish").(bool)),
	}

	if v, ok := d.GetOk("layers"); ok && len(v.([]interface{})) > 0 {
		funcParam.Layers = expandStringList(v.([]interface{}))
	}

	if v, ok := d.GetOk("dead_letter_config"); ok {
		dlcMaps := v.([]interface{})
		if len(dlcMaps) == 1 { // Schema guarantees either 0 or 1
			// Prevent panic on nil dead_letter_config. See GH-14961
			if dlcMaps[0] == nil {
				return fmt.Errorf("Nil dead_letter_config supplied for function: %s", functionName)
			}
			dlcMap := dlcMaps[0].(map[string]interface{})
			funcParam.DeadLetterConfig = &lambda.DeadLetterConfig{
				TargetArn: aws.String(dlcMap["target_arn"].(string)),
			}
		}
	}

	if v, ok := d.GetOk("vpc_config"); ok && len(v.([]interface{})) > 0 {
		config := v.([]interface{})[0].(map[string]interface{})

		funcParam.VpcConfig = &lambda.VpcConfig{
			SecurityGroupIds: expandStringSet(config["security_group_ids"].(*schema.Set)),
			SubnetIds:        expandStringSet(config["subnet_ids"].(*schema.Set)),
		}
	}

	if v, ok := d.GetOk("tracing_config"); ok {
		tracingConfig := v.([]interface{})
		tracing := tracingConfig[0].(map[string]interface{})
		funcParam.TracingConfig = &lambda.TracingConfig{
			Mode: aws.String(tracing["mode"].(string)),
		}
	}

	if v, ok := d.GetOk("environment"); ok {
		environments := v.([]interface{})
		environment, ok := environments[0].(map[string]interface{})
		if !ok {
			return errors.New("At least one field is expected inside environment")
		}

		if environmentVariables, ok := environment["variables"]; ok {
			variables := readEnvironmentVariables(environmentVariables.(map[string]interface{}))

			funcParam.Environment = &lambda.Environment{
				Variables: aws.StringMap(variables),
			}
		}
	}

	if v, ok := d.GetOk("kms_key_arn"); ok {
		funcParam.KMSKeyArn = aws.String(v.(string))
	}

	if v, exists := d.GetOk("tags"); exists {
		funcParam.Tags = tagsFromMapGeneric(v.(map[string]interface{}))
	}

	event := d.Get("event").([]interface{})[0].(map[string]interface{})

	// funcParam.VpcConfig = &lambda.VpcConfig{
	// 	SecurityGroupIds: expandStringSet(config["security_group_ids"].(*schema.Set)),
	// 	SubnetIds:        expandStringSet(config["subnet_ids"].(*schema.Set)),
	// }

	input := function.S3CreateFunctionInput{
		FunctionInput: funcParam,
		S3CreateEvent: function.S3CreateEvent{
			Bucket: aws.String(event["bucket"].(string)),
			Prefix: aws.String(event["prefix"].(string)),
			Suffix: aws.String(event["suffix"].(string)),
			Types:  aws.StringSlice(event["types"].([]string)),
			Key:    aws.String(event["key"].(string)),
		},
	}

	err := resource.Retry(1*time.Minute, func() *resource.RetryError {
		_, err := function.CreateFunction(input)

		if err != nil {
			log.Printf("[DEBUG] Error creating Lambda Function: %s", err)

			if isAWSErr(err, "InvalidParameterValueException", "The role defined for the function cannot be assumed by Lambda") {
				log.Printf("[DEBUG] Received %s, retrying CreateFunction", err)
				return resource.RetryableError(err)
			}
			if isAWSErr(err, "InvalidParameterValueException", "The provided execution role does not have permissions") {
				log.Printf("[DEBUG] Received %s, retrying CreateFunction", err)
				return resource.RetryableError(err)
			}
			if isAWSErr(err, "InvalidParameterValueException", "Your request has been throttled by EC2") {
				log.Printf("[DEBUG] Received %s, retrying CreateFunction", err)
				return resource.RetryableError(err)
			}
			if isAWSErr(err, "InvalidParameterValueException", "Lambda was unable to configure access to your environment variables because the KMS key is invalid for CreateGrant") {
				log.Printf("[DEBUG] Received %s, retrying CreateFunction", err)
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if err != nil {
		// 	if !isResourceTimeoutError(err) && !isAWSErr(err, "InvalidParameterValueException", "Your request has been throttled by EC2") {
		// 		return fmt.Errorf("Error creating Lambda function: %s", err)
		// 	}
		// 	// Allow additional time for slower uploads or EC2 throttling
		// 	err := resource.Retry(d.Timeout(schema.TimeoutCreate), func() *resource.RetryError {
		// 		_, err := conn.CreateFunction(params)
		// 		if err != nil {
		// 			log.Printf("[DEBUG] Error creating Lambda Function: %s", err)

		// 			if isAWSErr(err, "InvalidParameterValueException", "Your request has been throttled by EC2") {
		// 				log.Printf("[DEBUG] Received %s, retrying CreateFunction", err)
		// 				return resource.RetryableError(err)
		// 			}

		// 			return resource.NonRetryableError(err)
		// 		}
		// 		return nil
		// 	})
		// 	if isResourceTimeoutError(err) {
		// 		_, err = conn.CreateFunction(params)
		// 	}
		// 	if err != nil {
		// 		return fmt.Errorf("Error creating Lambda function: %s", err)
		// 	}
	}

	d.SetId(d.Get("function_name").(string))

	// if err := waitForLambdaFunctionCreation(conn, d.Id(), d.Timeout(schema.TimeoutCreate)); err != nil {
	// 	return fmt.Errorf("error waiting for Lambda Function (%s) creation: %s", d.Id(), err)
	// }

	return resourceFunctionS3Read(d, m)

}

func resourceFunctionS3Read(d *schema.ResourceData, m interface{}) error {
	return nil
}

func resourceFunctionS3Update(d *schema.ResourceData, m interface{}) error {
	return resourceFunctionS3Read(d, m)
}

func resourceFunctionS3Delete(d *schema.ResourceData, m interface{}) error {
	return nil
}