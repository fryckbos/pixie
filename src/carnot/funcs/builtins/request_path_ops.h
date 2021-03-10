#pragma once
#include <rapidjson/document.h>
#include <rapidjson/stringbuffer.h>
#include <rapidjson/writer.h>
#include <string>
#include <string_view>
#include <utility>
#include <vector>

#include "src/carnot/udf/registry.h"
#include "src/shared/types/types.h"

namespace pl {
namespace carnot {
namespace builtins {

/**
 * Registers UDF operations that work on request paths.
 * @param registry pointer to the registry.
 */
void RegisterRequestPathOpsOrDie(udf::Registry* registry);

class RequestPath {
  /**
   * This class assumes that the request paths "/a/b" and "a/b" are equivalent. And as a result,
   * RequestPath("a/b").ToString() == "/a/b".
   */
 public:
  explicit RequestPath(std::string request_path);
  RequestPath() = default;
  /**
   * Get the similarity of this request path to another one. The similarity metric used is the
   * number of path components that are the same, ignoring kAnyTokens, normalized by the total
   * number of path components.
   * @param other another request path to compare to this one.
   * @return similarity between 0.0 and 1.0.
   */
  //
  double Similarity(const RequestPath& other) const;

  /**
   * Updates i-th path component with new value.
   * @param i the index of the path component to update.
   * @param new_val The new value of the path componet.
   */
  void UpdatePathComponent(size_t i, const std::string& new_val) {
    DCHECK_LT(i, path_components_.size());
    path_components_[i] = new_val;
  }

  /**
   * Returns the request path unparsed.
   * Currently, the params of the request path are destroyed so
   * `RequestPath("/a?k=v").ToString() == "/a"`.
   * @return unparsed request path as a string, eg. "/a/b/c".
   */
  std::string ToString() const;

  /**
   * Returns whether this request path matches a template request path.
   * @param templ Tempalte request path to match against.
   * @return whether they match. If templ as any kAnyToken's as path components, then those path
   * components aren't considered. eg. /a/b/c matches the template /a/<*>/c but /a/<*>/c doesn't
   * match the template /a/b/c.
   */
  bool Matches(const RequestPath& templ) const;

  template <typename H>
  friend H AbslHashValue(H h, const RequestPath& request_path) {
    return H::combine(std::move(h), request_path.ToString());
  }

  // Serialization/Deserialization
  std::string ToJSON() const;
  void ToJSON(rapidjson::Writer<rapidjson::StringBuffer>* writer) const;
  static RequestPath FromJSON(std::string serialized_request_path);
  static RequestPath FromJSON(const rapidjson::Document::ValueType& doc);

  int64_t depth() const { return path_components_.size(); }
  const std::vector<std::string>& path_components() const { return path_components_; }
  inline static constexpr char kAnyToken[] = "*";

 private:
  std::vector<std::string> path_components_;
};

bool operator==(const RequestPath& a, const RequestPath& b);

class RequestPathCluster {
  /**
   * This class represents a single cluster of request paths. However, if the cluster has less
   unique members than the specified minimum cardinality, then it acts as if each member is its own
   cluster, until the minimum cardinality is reached.
   * For example, suppose the cluster
   consists of request paths: "/a/b/a", "/a/b/b", "/a/b/c" and the minimum cardinality is 5, then
   since there are only 3 unique members in the cluster, if Predict is called for this cluster and
   the request path to predict the cluster for is "/a/b/b", predict will return "/a/b/b" since
   "/a/b/b" is treated as its own cluster until the minimum cardinality is reached. If 3 more unique
   members were added to the cluster, bringing the cardinality above the minimum, then Predict with
   the same request path would return the cluster centroid since the members are no longer treated
   as individual clusters.

   * The so-called "centroid" of the cluster is the longest common subsequence of all members of the
   cluster, with differences replaced by RequestPath::kAnyToken. eg. if the members are /a/b/c and
   /a/f/c, then the centroid is /a/<RequestPath::kAnyToken>/c.
   */
 public:
  explicit RequestPathCluster(const RequestPath& request_path, size_t min_cardinality = 5)
      : centroid_(request_path), min_cardinality_(min_cardinality) {
    members_.insert(request_path);
  }

  /**
   * Merge another cluster into this one.
   * @param other_cluster cluster to merge into this one.
   */
  void Merge(const RequestPathCluster& other_cluster);

  /**
   * Returns the similarity of the given request_path to the centroid of this cluster. See
   * RequestPath::Similarity for definition of the similarity metric.
   * @param request_path request path to compare to the cluster centroid.
   * @return similarity between 0.0 and 1.0
   */
  double Similarity(const RequestPath& request_path) const {
    return centroid_.Similarity(request_path);
  }

  /**
   * Returns the request path of the cluster centroid that matches the passed in request path.
   * if this cluster hasn't reached the minimum cardinality then this will return the member that
   * matches the request path, otherwise it will just return the cluster centroid. centroid,
   * @param request_path the request path to check against the members of this cluster.
   * @return the matching member or cluster centroid.
   */
  const RequestPath& Predict(const RequestPath& request_path) const;

  // Serialization/Deserialization
  static RequestPathCluster FromJSON(const std::string& json);
  static RequestPathCluster FromJSON(const rapidjson::Document::ValueType& doc);
  std::string ToJSON() const;
  void ToJSON(rapidjson::Writer<rapidjson::StringBuffer>* writer) const;

  const RequestPath& centroid() const { return centroid_; }

 private:
  explicit RequestPathCluster(size_t min_cardinality = 5) : min_cardinality_(min_cardinality) {}

  void MergeCentroids(const RequestPath& other_centroid);
  void MergeMembers(const absl::flat_hash_set<RequestPath>& other_members);

  inline static constexpr char kCentroidKey[] = "c";
  inline static constexpr char kMembersKey[] = "m";
  RequestPath centroid_;
  size_t min_cardinality_;
  absl::flat_hash_set<RequestPath> members_;
};

class RequestPathClustering {
 public:
  static RequestPathClustering FromJSON(const std::string& json);

  std::string ToJSON() const;

  /**
   * @param request_path request path to get prediction for.
   * @return the centroid of the cluster closest to the given request path.
   */
  const RequestPath& Predict(const RequestPath& request_path);

  /**
   * Updates the clustering given a new cluster to be added.
   * The new cluster can be a single point cluster, or a larger cluster to merge in.
   * @param new_cluster New cluster to update the clustering for.
   */
  void Update(const RequestPathCluster& new_cluster);

  const std::vector<RequestPathCluster>& clusters() const { return clusters_; }

 private:
  double MaxSimilarity(const RequestPath& request_path, int64_t* max_index);
  void AddNewCluster(const RequestPathCluster& cluster);
  void MergeCluster(int64_t cluster_index, const RequestPathCluster& other_cluster);
  // We currently only allow request path's with the same depth to be clustered together.
  absl::flat_hash_map<int64_t, std::vector<int64_t>> depth_to_centroid_indices_;
  std::vector<RequestPathCluster> clusters_;
  double thresh_ = 0.5;
};

class RequestPathClusteringPredictUDF : public udf::ScalarUDF {
 public:
  StringValue Exec(FunctionContext*, StringValue request_path_str,
                   StringValue serialized_clustering) {
    if (!clustering_init_) {
      clustering_ = RequestPathClustering::FromJSON(serialized_clustering);
      clustering_init_ = true;
    }
    auto request_path = RequestPath(request_path_str);
    return clustering_.Predict(request_path).ToString();
  }

  RequestPathClustering clustering_;
  bool clustering_init_ = false;
};

class RequestPathClusteringFitUDA : public udf::UDA {
 public:
  void Update(FunctionContext*, StringValue request_path_str) {
    auto request_path = RequestPath(request_path_str);
    clustering_.Update(RequestPathCluster(request_path));
  }
  void Merge(FunctionContext*, const RequestPathClusteringFitUDA& other) {
    for (const auto& cluster : other.clustering_.clusters()) {
      clustering_.Update(cluster);
    }
  }
  StringValue Finalize(FunctionContext*) { return clustering_.ToJSON(); }

  StringValue Serialize(FunctionContext*) { return clustering_.ToJSON(); }

  Status Deserialize(FunctionContext*, const StringValue& data) {
    clustering_ = RequestPathClustering::FromJSON(data);
    return Status::OK();
  }

 private:
  RequestPathClustering clustering_;
};

class RequestPathEndpointMatcherUDF : public udf::ScalarUDF {
 public:
  BoolValue Exec(FunctionContext*, StringValue request_path, StringValue endpoint) {
    return RequestPath(request_path).Matches(RequestPath(endpoint));
  }
};

}  // namespace builtins
}  // namespace carnot
}  // namespace pl
