// Copyright 2020 Nelson Elhage
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

package bootstrap

const CFTemplate = `{
  "Parameters": {
    "ObjectStoreBucket": {
      "Type": "String",
      "Description": "A pre-existing S3 bucket to use for llama's object store"
    },
    "ObjectStorePrefix": {
      "Type": "String",
      "Description": "A prefix in $ObjectStoreBucket under which to store objects",
      "Default": "/",
      "AllowedPattern": "/([a-zA-Z0-9_/-]*/)?",
      "ConstraintDescription": "must be an S3 path prefix starting and ending with /"
    },
    "ECRRepositoryName": {
      "Type": "String",
      "Description": "The name for the llama ECR repository",
      "Default": "llama",
      "AllowedPattern": "(?:[a-z0-9]+(?:[._-][a-z0-9]+)*/)*[a-z0-9]+(?:[._-][a-z0-9]+)*",
      "ConstraintDescription": "must be a valid ECR repository name"
    }
  },
  "Resources": {
    "LlamaRole": {
      "Type": "AWS::IAM::Role",
      "Properties": {
        "AssumeRolePolicyDocument": {
          "Version": "2012-10-17",
          "Statement": [
            {
              "Effect": "Allow",
              "Principal": {
                "Service": "lambda.amazonaws.com"
              },
              "Action": "sts:AssumeRole"
            }
          ]
        },
        "Description": "The role used to invoke llama Lambda functions",
        "ManagedPolicyArns": [
          "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
        ],
        "Policies": [
          {
            "PolicyName": "llama-access-object-store",
            "PolicyDocument": {
              "Version": "2012-10-17",
              "Statement": [
                {
                  "Sid": "LlamaAccessObjectStore",
                  "Effect": "Allow",
                  "Action": [
                    "s3:PutObject",
                    "s3:GetObject",
                    "s3:ListBucketMultipartUploads",
                    "s3:ListBucket"
                  ],
                  "Resource": [
                    {
                      "Fn::Join": [
                        "",
                        [
                          "arn:aws:s3:::",
                          {"Ref": "ObjectStoreBucket"}
                        ]
                      ]
                    },
                    {
                      "Fn::Join": [
                        "",
                        [
                          "arn:aws:s3:::",
                          {"Ref": "ObjectStoreBucket"},
                          {"Ref": "ObjectStorePrefix"},
                          "*"
                        ]
                      ]
                    }
                  ]
                }
              ]
            }
          }
        ]
      }
    },
    "LlamaRegistry": {
      "Type": "AWS::ECR::Repository",
      "Properties": {
        "RepositoryName": {"Ref": "ECRRepositoryName"}
      }
    },
  }
}`
