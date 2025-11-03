package dfs

import (
	"fmt"
	"sync"
)

type Node struct {
	ID       uint64
	Parent   *Node
	Children []*Node
}

type Tree struct {
	Root  *Node
	Nodes map[uint64]*Node
	mutex sync.RWMutex
}

func NewTree() *Tree {
	tree := &Tree{
		Root: &Node{
			ID:       1,
			Children: []*Node{},
			Parent:   nil,
		},
		Nodes: make(map[uint64]*Node),
	}
	// 将根节点添加到映射中
	tree.Nodes[1] = tree.Root
	return tree
}

// Get 根据ID获取节点
func (t *Tree) Get(id uint64) (*Node, bool) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	node, exists := t.Nodes[id]
	return node, exists
}

// Insert 插入新节点到指定父节点
func (t *Tree) Insert(parentID uint64, nodeID uint64) (*Node, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// 检查节点ID是否已存在
	if _, exists := t.Nodes[nodeID]; exists {
		logger.Errorf("node with id %d already exists", nodeID)
		return nil, fmt.Errorf("node with id %d already exists", nodeID)
	}

	// 获取父节点
	parent, exists := t.Nodes[parentID]
	if !exists {
		logger.Errorf("parent node with id %d not found", parentID)
		return nil, fmt.Errorf("parent node with id %d not found", parentID)
	}

	// 创建新节点
	newNode := &Node{
		ID:       nodeID,
		Parent:   parent,
		Children: []*Node{},
	}

	// 添加到父节点的子节点列表
	parent.Children = append(parent.Children, newNode)

	// 添加到节点映射
	t.Nodes[nodeID] = newNode

	return newNode, nil
}

// Remove 删除指定ID的节点及其所有子节点
func (t *Tree) Remove(id uint64) error {
	// 不允许删除根节点
	if id == t.Root.ID {
		logger.Errorf("cannot remove root node")
		return fmt.Errorf("cannot remove root node")
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	// 获取要删除的节点
	node, exists := t.Nodes[id]
	if !exists {
		logger.Errorf("node with id %d not found", id)
		return fmt.Errorf("node with id %d not found", id)
	}

	// 获取父节点
	parent := node.Parent
	if parent == nil {
		logger.Errorf("node with id %d has no parent", id)
		return fmt.Errorf("node with id %d has no parent", id)
	}

	// 递归删除所有子节点
	removeChildren(t, node)

	// 从父节点的子节点列表中移除
	for i, child := range parent.Children {
		if child.ID == id {
			parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
			break
		}
	}

	// 从映射中删除节点
	delete(t.Nodes, id)

	return nil
}

// removeChildren 递归删除节点及其所有子节点
func removeChildren(t *Tree, node *Node) {
	// 递归删除所有子节点
	for _, child := range node.Children {
		removeChildren(t, child)
	}
	// 从映射中删除节点
	delete(t.Nodes, node.ID)
}
